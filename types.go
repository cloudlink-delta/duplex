package duplex

import (
	"sync"

	"github.com/goccy/go-json"
	peer "github.com/muka/peerjs-go"
)

// Peer is a representation of a peer connection for a duplex instance.
type Peer struct {
	Parent               *Instance      // Pointer to the parent instance that created this peer
	Lock                 *sync.Mutex    // Lock for transmit thread safety
	KeyStore             map[string]any // Map of key-value pairs of any type
	*peer.DataConnection                // Pointer to the peer data connection
}

// Instance is a representation of a duplex instance.
type Instance struct {
	Name             string
	Handler          *peer.Peer
	Close            chan bool
	Done             chan bool
	RetryCounter     int
	MaxRetries       int
	Peers            Peers
	OnOpen           func(*Peer)
	OnClose          func(*Peer)
	CustomHandlers   map[string]func(*Peer, *RxPacket)
	RemappedHandlers map[string]func(*Peer, *RxPacket)
	IsBridge         bool
	IsRelay          bool
	IsDiscovery      bool
}

type Packet struct {
	Opcode string `json:"opcode"`
	Origin string `json:"origin,omitempty"`
	Target string `json:"target,omitempty"`
	TTL    int    `json:"ttl,omitempty"`
	Id     string `json:"id,omitempty"`
	Method string `json:"method,omitempty"`
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
