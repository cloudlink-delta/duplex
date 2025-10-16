package duplex

import (
	"log"

	peer "github.com/muka/peerjs-go"
	"github.com/pion/webrtc/v3"
)

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
		Name:         ID,
		Handler:      serverPeer,
		Close:        make(chan bool),
		Done:         make(chan bool),
		RetryCounter: 0,
		MaxRetries:   5,
	}

	return instance
}

func (s *Instance) Run() {
	provider := s.Handler
	defer provider.Destroy()

	provider.On("connection", func(data any) {
		switch c := data.(type) {
		case *peer.DataConnection:
			s.PeerHandler(&Peer{DataConnection: c, Parent: s})
		default:
			panic("unhandled data type")
		}
	})

	provider.On("error", func(data any) {
		log.Printf("Peer error: %v", data)
	})

	provider.On("open", func(data any) {
		s.RetryCounter = 0
		log.Printf("Peer opened as %s", s.Name)
	})

	provider.On("close", func(data any) {
		log.Println("Peer closed")
		s.Done <- true
	})

	<-s.Close
	log.Println("\nPeer got close signal")
}

func (s *Instance) PeerHandler(conn *Peer) {
	conn.On("open", func(data any) {
		log.Printf("%s connected", conn.GiveName())
		log.Printf("%s metadata: %v", conn.GiveName(), conn.Metadata)
	})

	conn.On("close", func(data any) {
		log.Printf("%s disconnected", conn.GiveName())
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
