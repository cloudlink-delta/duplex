package duplex

import (
	"log"
	"slices"
	"sync"
	"time"

	peer "github.com/muka/peerjs-go"
	"github.com/muka/peerjs-go/enums"
	"github.com/pion/webrtc/v3"
)

type Config struct {
	Hostname     string
	Secure       bool
	Port         int
	ICEServers   []webrtc.ICEServer
	EnablePinger bool
	PingInterval int64 // in milliseconds
	VeryVerbose  bool
}

type Peers map[string]*Peer
type PeerSlice []*Peer

func New(ID string, args *Config) *Instance {
	config := configure(args)
	return &Instance{
		Name:                             ID,
		Pinger:                           args.EnablePinger,
		PingInterval:                     time.Duration(args.PingInterval) * time.Millisecond,
		Close:                            make(chan bool),
		Done:                             make(chan bool),
		RetryCounter:                     0,
		MaxRetries:                       5,
		Peers:                            make(Peers),
		CustomHandlersRequiredFeatures:   make(map[string][]string),
		CustomHandlers:                   make(map[string]func(*Peer, *RxPacket)),
		RemappedHandlersRequiredFeatures: make(map[string][]string),
		RemappedHandlers:                 make(map[string]func(*Peer, *RxPacket)),
		peerjs_config:                    config,
	}
}

func configure(args *Config) peer.Options {
	config := peer.NewOptions()

	if args == nil {
		args = &Config{}
	}

	if args.VeryVerbose {
		config.Debug = 3
	} else {
		config.Debug = 2
	}

	if len(args.Hostname) > 0 {
		config.Host = args.Hostname
		config.Secure = args.Secure
		config.Port = args.Port
	} else {
		config.Host = "peerjs.mikedev101.cc"
		config.Secure = true
		config.Port = 443
	}

	if len(args.ICEServers) > 0 {
		config.Configuration.ICEServers = args.ICEServers
	} else {
		config.Configuration.ICEServers = []webrtc.ICEServer{
			{
				// All CloudLink-provided STUN servers
				URLs: []string{
					"stun:vpn.mikedev101.cc:3478",
					"stun:vpn.mikedev101.cc:5349",
				},
			},
			{
				// All CloudLink-provided TURN servers
				URLs: []string{
					"turn:vpn.mikedev101.cc:5349",
					"turn:vpn.mikedev101.cc:3478",
				},
				Username:   "free",
				Credential: "free",
			},
			{
				// Other STUN servers
				URLs: []string{
					"stun:stun.l.google.com:19302",
					"stun:stun.l.google.com:5349",
					"stun:stun1.l.google.com:3478",
					"stun:stun1.l.google.com:5349",
					"stun:stun2.l.google.com:19302",
					"stun:stun2.l.google.com:5349",
					"stun:stun3.l.google.com:3478",
					"stun:stun3.l.google.com:5349",
					"stun:stun4.l.google.com:19302",
					"stun:stun4.l.google.com:5349",
					"stun:stun.cloudflare.com:3478",
					"stun:stun.cloudflare.com:53",
				},
			},
		}
	}
	return config
}

func (p *Peers) ToSlice(exclusions ...*Peer) PeerSlice {
	var peers PeerSlice
	for _, peer := range *p {
		if !slices.Contains(exclusions, peer) {
			peers = append(peers, peer)
		}
	}
	return peers
}

func (i *Instance) AttemptReconnect() {
	i.mu.Lock()
	if i.isReconnecting {
		i.mu.Unlock()
		return
	}
	if i.RetryCounter >= i.MaxRetries {
		log.Printf("Max retries (%d) reached. Giving up.", i.MaxRetries)
		i.mu.Unlock()
		return
	}
	i.isReconnecting = true

	// Nuke the reference immediately so nothing else uses the dead handler
	if i.Handler != nil {
		i.Handler.Destroy()
		i.Handler = nil
	}
	i.mu.Unlock()

	go func() {
		for {
			i.mu.Lock()
			currentRetry := i.RetryCounter
			i.mu.Unlock()

			log.Printf("Re-initialization attempt #%d...", currentRetry+1)

			// 1. Create a channel to catch the setup result
			type setupResult struct {
				err error
			}
			done := make(chan setupResult, 1)

			go func() {
				err := i.setup()
				done <- setupResult{err}
			}()

			// 2. Wait for setup or a timeout
			select {
			case res := <-done:
				if res.err == nil {
					// setup() handles resetting i.isReconnecting on "open"
					return
				}
				log.Printf("Setup failed: %v", res.err)
			case <-time.After(15 * time.Second):
				log.Printf("Setup timed out (network still unreachable)")
			}

			// 3. Prepare for next loop
			i.mu.Lock()
			i.RetryCounter++
			if i.RetryCounter >= i.MaxRetries {
				i.isReconnecting = false
				i.mu.Unlock()
				return
			}
			i.mu.Unlock()

			// 4. Backoff
			time.Sleep(5 * time.Second)
		}
	}()
}

func (i *Instance) setup() error {

	// 1. Create the Peer
	p, err := peer.NewPeer(i.Name, i.peerjs_config)
	if err != nil {
		return err
	}

	// 2. Bind Connection Listener
	p.On("connection", func(data any) {
		if c, ok := data.(*peer.DataConnection); ok {
			p := &Peer{
				DataConnection: c,
				Parent:         i,
				Lock:           &sync.Mutex{},
				KeyStore:       make(map[string]any),
				KeyLock:        &sync.Mutex{},
				OpcodeMatchers: make(map[*Peer]*OpcodeMatcher),
				Listeners:      make(map[string]Listener),
				IsInitiator:    false,
				Done:           make(chan bool),
			}
			i.PeerHandler(p)
		}
	})

	// 3. Bind Error Listener
	p.On("error", func(data any) {
		errMsg, ok := data.(peer.PeerError)
		if !ok {
			return
		}

		switch errMsg.Type {
		case enums.PeerErrorTypeNetwork, enums.PeerErrorTypeServerError,
			enums.PeerErrorTypeSocketError, enums.PeerErrorTypeSocketClosed,
			enums.PeerErrorTypeDisconnected:
			log.Printf("Recoverable error: %v. Triggering reconnect...", errMsg.Type)
			i.AttemptReconnect()

		case enums.PeerErrorTypeUnavailableID:
			log.Println("ID already in use. Try again later.")
			go func() {
				i.Close <- true
			}()
			return

		case enums.PeerErrorTypeSslUnavailable,
			enums.PeerErrorTypeBrowserIncompatible,
			enums.PeerErrorTypeInvalidID,
			enums.PeerErrorTypeInvalidKey:
			log.Printf("Fatal: %v. Manual intervention required.", errMsg.Type)
			go func() {
				i.Close <- true
			}()
			return
		default:
			log.Printf("Non-critical or unhandled peer error: %v", errMsg.Type)
		}
	})

	// 4. Bind Open Listener
	p.On("open", func(data any) {
		i.mu.Lock()
		i.isReconnecting = false
		i.RetryCounter = 0
		i.active_time_start = time.Now()
		i.mu.Unlock()

		log.Printf("Peer opened successfully as %s", i.Name)
		if i.OnCreate != nil {
			i.OnCreate()
		}
	})

	// 5. Bind Close Listener
	p.On("close", func(data any) {
		log.Println("Peer connection closed.")
		i.mu.Lock()
		i.active_time_start = time.Time{}
		i.mu.Unlock()
		// If we didn't intend to close, try to reconnect
		i.AttemptReconnect()
	})

	// Store the new handler
	i.mu.Lock()
	i.Handler = p
	i.mu.Unlock()

	return nil
}

func (i *Instance) Run() {
	log.Println("Initializing peer...")

	// Start the connection process
	if err := i.setup(); err != nil {
		log.Printf("Initial connection failed: %v. Reconnect loop will take over.", err)
		i.AttemptReconnect()
	}

	log.Println("Peer instance is running...")
	<-i.Close

	log.Println("Shutting down peer instance...")
	if i.Handler != nil {
		i.Handler.Destroy()
	}
	i.Done <- true
}

func (i *Instance) GetPeerState() PeerState {
	i.mu.Lock()
	defer i.mu.Unlock()

	state := PeerState{
		ConnectionState: !i.isReconnecting && !i.active_time_start.IsZero(),
	}
	if state.ConnectionState {
		state.Uptime = time.Since(i.active_time_start).Round(time.Second).String()
	}

	return state
}

func (i *Instance) Connect(id string) *Peer {
	options := peer.NewConnectionOptions()
	options.Label = "default"
	options.Reliable = true
	options.Serialization = "json"
	options.Metadata = map[string]any{
		"protocol": "delta",
		"name":     i.Name,
	}

	conn, err := i.Handler.Connect(id, options)
	if err != nil {
		log.Printf("Failed to connect to peer %s: %v", id, err)
		return nil
	}

	p := &Peer{
		DataConnection: conn,
		Parent:         i,
		Lock:           &sync.Mutex{},
		KeyStore:       make(map[string]any),
		KeyLock:        &sync.Mutex{},
		OpcodeMatchers: make(map[*Peer]*OpcodeMatcher),
		Listeners:      make(map[string]Listener),
		IsInitiator:    true,
		Done:           make(chan bool),
	}

	i.PeerHandler(p)
	return p
}

func (i *Instance) SpawnTicker(conn *Peer) {
	if !i.Pinger {
		return
	}
	ticker := time.NewTicker(i.PingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			conn.Write(&TxPacket{
				Packet: Packet{
					Opcode: "PING",
					TTL:    1,
				},
				Payload: map[string]int64{"t1": time.Now().UnixNano() / 1000000},
			})
		case <-conn.Done:
			return
		}
	}
}

func (i *Instance) PeerHandler(conn *Peer) {
	conn.On("open", func(data any) {
		log.Printf("%s connected", conn.GiveName())
		log.Printf("%s metadata: %v", conn.GiveName(), conn.Metadata)
		i.Peers[conn.GetPeerID()] = conn

		if conn.IsInitiator {
			conn.SendNegotiate(&RxPacket{})

			// Start periodic ping
			go i.SpawnTicker(conn)
		}

		if fn := i.OnOpen; fn != nil {
			fn(conn)
		}
	})

	conn.On("close", func(data any) {
		log.Printf("%s disconnected", conn.GiveName())
		delete(i.Peers, conn.GetPeerID())
		select {
		case <-conn.Done:
		default:
			close(conn.Done) // Signal all goroutines tied to this peer to cleanly exit
		}
		if fn := i.OnClose; fn != nil {
			fn(conn)
		}
	})

	conn.On("error", func(data any) {
		log.Printf("%s error: %v", conn.GiveName(), data)
	})

	conn.On("data", func(data any) {
		packet := conn.Read(data)
		if packet == nil {
			return
		}
		log.Printf("%s 🢂  %v", conn.GiveName(), packet)
		go conn.HandlePacket(packet)
	})
}

func (i *Instance) Bind(opcode string, handler func(*Peer, *RxPacket), required_features ...string) {
	if _, exists := i.CustomHandlers[opcode]; exists {
		log.Printf("Handler for opcode %s already exists", opcode)
		return
	}
	i.CustomHandlers[opcode] = handler
	if len(required_features) > 0 {
		i.CustomHandlersRequiredFeatures[opcode] = required_features
	}
}

func (i *Instance) Remap(opcode string, handler func(*Peer, *RxPacket), required_features ...string) {
	if _, exists := i.RemappedHandlers[opcode]; exists {
		log.Printf("Remapped handler for opcode %s already exists", opcode)
		return
	}
	i.RemappedHandlers[opcode] = handler
	if len(required_features) > 0 {
		i.RemappedHandlersRequiredFeatures[opcode] = required_features
	}
}

func (i *Instance) Unbind(opcode string) {
	delete(i.CustomHandlers, opcode)
}

func (i *Instance) Unmap(opcode string) {
	delete(i.RemappedHandlers, opcode)
}

func (i *Instance) Broadcast(packet *TxPacket, peers PeerSlice) {
	for _, peer := range peers {
		go peer.Write(packet)
	}
}
