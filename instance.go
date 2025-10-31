package duplex

import (
	"log"
	"slices"
	"sync"

	peer "github.com/muka/peerjs-go"
	"github.com/pion/webrtc/v3"
)

type Peers map[string]*Peer
type PeerSlice []*Peer

func New(ID string) *Instance {
	config := peer.NewOptions()
	config.PingInterval = 500
	config.Debug = 2
	config.Host = "peerjs.mikedev101.cc"
	config.Port = 443
	config.Secure = true
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

	log.Println("Opening peer...")
	serverPeer, err := peer.NewPeer(ID, config)
	if err != nil {
		log.Println(err)
		return nil
	}

	instance := &Instance{
		Name:                             ID,
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
			}
			i.PeerHandler(p)
		default:
			panic("unhandled data type")
		}
	})

	provider.On("error", func(data any) {
		log.Printf("Peer error: %v", data)
	})

	provider.On("open", func(data any) {
		i.RetryCounter = 0
		log.Printf("Peer opened as %s", i.Name)
	})

	provider.On("close", func(data any) {
		log.Println("Peer closed")
		i.Done <- true
	})

	<-i.Close
	log.Println("\nPeer got close signal")
}

func (i *Instance) PeerHandler(conn *Peer) {
	conn.On("open", func(data any) {
		log.Printf("%s connected", conn.GiveName())
		log.Printf("%s metadata: %v", conn.GiveName(), conn.Metadata)
		i.Peers[conn.GetPeerID()] = conn
		if fn := i.OnOpen; fn != nil {
			fn(conn)
		}
	})

	conn.On("close", func(data any) {
		log.Printf("%s disconnected", conn.GiveName())
		delete(i.Peers, conn.GetPeerID())
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
		log.Printf("%s 🢂 %v", conn.GiveName(), packet)
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
