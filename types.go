package duplex

import (
	"sync"

	"github.com/goccy/go-json"
	peer "github.com/muka/peerjs-go"
)

type Listener struct {
	Opcode  string
	Handler func(*RxPacket)
}

// Peer is a representation of a peer connection for a duplex instance.
type Peer struct {
	Parent               *Instance            // Pointer to the parent instance that created this peer
	Lock                 *sync.Mutex          // Lock for thread safety
	KeyStore             map[string]any       // Map of key-value pairs of any type
	Listeners            map[string]*Listener // Map of key-value pairs to listeners.
	Features             []string             // List of features advertised by this peer
	*peer.DataConnection                      // Pointer to the peer data connection
}

// Instance is a representation of a duplex instance.
type Instance struct {
	Name                             string
	Handler                          *peer.Peer
	Close                            chan bool
	Done                             chan bool
	RetryCounter                     int
	MaxRetries                       int
	Peers                            Peers
	AfterNegotiation                 func(*Peer)
	OnOpen                           func(*Peer)
	OnClose                          func(*Peer)
	CustomHandlersRequiredFeatures   map[string][]string
	CustomHandlers                   map[string]func(*Peer, *RxPacket)
	RemappedHandlersRequiredFeatures map[string][]string
	RemappedHandlers                 map[string]func(*Peer, *RxPacket)
	IsBridge                         bool
	IsRelay                          bool
	IsDiscovery                      bool
}

type Packet struct {
	Opcode   string `json:"opcode"`
	Origin   string `json:"origin,omitempty"`
	Target   string `json:"target,omitempty"`
	TTL      int    `json:"ttl,omitempty"`
	Id       string `json:"id,omitempty"`
	Method   string `json:"method,omitempty"`
	Listener string `json:"listener,omitempty"`
}

type RxPacket struct {
	Packet
	Payload json.RawMessage `json:"payload,omitempty"`
}

func (p *RxPacket) String() string {
	resp, _ := json.Marshal(p)
	return string(resp)
}

type TxPacket struct {
	Packet
	Payload any `json:"payload,omitempty"`
}

func (p *TxPacket) String() string {
	resp, _ := json.Marshal(p)
	return string(resp)
}

type NegotiationArgs struct {
	Version     VersionArgs `json:"version"`
	SpecVersion int         `json:"spec_version"`
	Plugins     []string    `json:"plugins"`
	IsBridge    bool        `json:"is_bridge"`
	IsRelay     bool        `json:"is_relay"`
	IsDiscovery bool        `json:"is_discovery"`
}

type VersionArgs struct {
	Type  string `json:"type"`
	Major int    `json:"major"`
	Minor int    `json:"minor"`
	Patch int    `json:"patch"`
}
