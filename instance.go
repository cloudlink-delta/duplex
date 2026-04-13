package duplex

import (
	"fmt"
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
				URLs: []string{"stun:vpn.mikedev101.cc:3478", "stun:vpn.mikedev101.cc:5349"},
			},
			{
				URLs:       []string{"turn:vpn.mikedev101.cc:5349", "turn:vpn.mikedev101.cc:3478"},
				Username:   "free",
				Credential: "free",
			},
		}
	}

	log.Println("Opening peer...")

	var serverPeer *peer.Peer
	var err error

	for i := range 5 {
		type result struct {
			p   *peer.Peer
			err error
		}
		c := make(chan result, 1)
		go func() {
			p, e := peer.NewPeer(ID, config)
			if e != nil {
				c <- result{nil, e}
				return
			}

			wait := make(chan error, 1)
			var once sync.Once

			p.On("open", func(data any) {
				once.Do(func() { wait <- nil })
			})
			p.On("error", func(data any) {
				once.Do(func() { wait <- fmt.Errorf("%v", data) })
			})

			if e = <-wait; e != nil {
				p.Destroy()
				c <- result{nil, e}
			} else {
				c <- result{p, nil}
			}
		}()

		select {
		case res := <-c:
			err = res.err
			if err == nil {
				serverPeer = res.p
				goto Success
			}
		case <-time.After(10 * time.Second):
			err = fmt.Errorf("connection timed out")
		}

		log.Printf("Failed to open peer (attempt %d/5): %v", i+1, err)
		time.Sleep(2 * time.Second)
	}

	if err != nil {
		panic(err)
	}

Success:

	instance := &Instance{
		Name:                             ID,
		Pinger:                           args.EnablePinger,
		PingInterval:                     time.Duration(args.PingInterval) * time.Millisecond,
		Handler:                          serverPeer,
		Close:                            make(chan bool),
		Done:                             make(chan bool),
		RetryCounter:                     0,
		MaxRetries:                       5,
		Peers:                            make(Peers),
		CustomHandlersRequiredFeatures:   make(map[string][]string),
		CustomHandlers:                   make(map[string]func(*Peer, *RxPacket)),
		RemappedHandlersRequiredFeatures: make(map[string][]string),
		RemappedHandlers:                 make(map[string]func(*Peer, *RxPacket)),
	}

	return instance
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
	if i.isReconnecting || i.RetryCounter >= i.MaxRetries {
		i.mu.Unlock()
		return
	}
	i.isReconnecting = true
	i.mu.Unlock()

	go func() {
		for {
			i.mu.Lock()
			if i.RetryCounter >= i.MaxRetries {
				log.Printf("Max reconnect attempts (%d) reached. Giving up.", i.MaxRetries)
				i.isReconnecting = false
				i.mu.Unlock()
				return
			}
			i.RetryCounter++
			currentAttempt := i.RetryCounter
			i.mu.Unlock()

			log.Printf("Attempting session reconnect #%d/%d...", currentAttempt, i.MaxRetries)

			err := i.Handler.Reconnect()
			if err == nil {
				// If Reconnect() didn't return an immediate error,
				// we wait to see if the "open" event fires.
				// We sleep here to prevent rapid-fire attempts if the socket
				// opens but immediately closes again.
				time.Sleep(5 * time.Second)
			} else {
				log.Printf("Reconnect call failed: %v", err)
				time.Sleep(5 * time.Second)
			}

			// Check if we were successful (the 'open' handler resets isReconnecting)
			i.mu.Lock()
			if !i.isReconnecting {
				i.mu.Unlock()
				return
			}
			i.mu.Unlock()
		}
	}()
}

func (i *Instance) Run() {
	provider := i.Handler
	defer provider.Destroy()

	provider.On("connection", func(data any) {
		switch c := data.(type) {
		case *peer.DataConnection:
			p := &Peer{
				DataConnection: c,
				Parent:         i,
				Lock:           &sync.Mutex{},
				KeyStore:       make(map[string]any),
				OpcodeMatchers: make(map[*Peer]*OpcodeMatcher),
				Listeners:      make(map[string]Listener),
				IsInitiator:    false,
				Done:           make(chan bool),
			}
			i.PeerHandler(p)
		default:
			panic("unhandled data type")
		}
	})

	provider.On("error", func(data any) {
		errMsg, ok := data.(peer.PeerError)
		if !ok {
			return
		}

		switch errMsg.Type {
		case enums.PeerErrorTypeNetwork,
			enums.PeerErrorTypeServerError,
			enums.PeerErrorTypeSocketError,
			enums.PeerErrorTypeSocketClosed,
			enums.PeerErrorTypeDisconnected:

			log.Printf("Recoverable error detected: %v", errMsg.Type)
			i.AttemptReconnect()

		case enums.PeerErrorTypeUnavailableID:
			log.Println("ID already in use. Manual intervention required.")

		case enums.PeerErrorTypeSslUnavailable,
			enums.PeerErrorTypeBrowserIncompatible,
			enums.PeerErrorTypeInvalidID,
			enums.PeerErrorTypeInvalidKey:
			log.Fatalf("Fatal: %v. Manual intervention required.", errMsg.Type)

		default:
			log.Printf("Non-critical or unhandled error: %v", errMsg.Type)
		}
	})

	var once sync.Once
	triggerOpen := func() {
		once.Do(func() {
			i.RetryCounter = 0
			log.Printf("Peer opened as %s", i.Name)
			if fn := i.OnCreate; fn != nil {
				fn()
			}
		})
	}

	provider.On("open", func(data any) {
		triggerOpen()
	})

	triggerOpen()

	provider.On("close", func(data any) {
		log.Println("Peer closed")
		i.Done <- true
	})

	<-i.Close
	log.Println("\nPeer got close signal")
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
