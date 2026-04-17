package duplex

import (
	"sync"
	"time"

	peer "github.com/cloudlink-delta/peerjs-go"
	"github.com/goccy/go-json"
)

type Listener func(*RxPacket)
type OpcodeMatcher struct {
	Opcodes  []string
	Callback func(*RxPacket)
}

// Peer is a representation of a peer connection for a duplex instance.
type Peer struct {
	Parent               *Instance      // Pointer to the parent instance that created this peer
	Lock                 *sync.Mutex    // Lock for thread safety
	KeyStore             map[string]any // Map of key-value pairs of any type
	KeyLock              *sync.Mutex
	OpcodeMatchers       map[*Peer]*OpcodeMatcher // Map of key-value pairs to listen to specific opcodes from specific peers.
	Listeners            map[string]Listener      // Map of key-value pairs to listeners.
	Features             []string                 // List of features advertised by this peer
	IsInitiator          bool                     // True if this peer initiated the connection
	IsBridge             bool                     // True if this peer is a bridge
	IsRelay              bool                     // True if this peer is a relay
	IsDiscovery          bool                     // True if this peer is a discovery
	Done                 chan bool                // Channel to signal connection closure
	RTT                  int64                    // Round-trip time (in milliseconds)
	GiveNameRemapper     func() string
	*peer.DataConnection // Pointer to the peer data connection
}

// Instance is a representation of a duplex instance.
type Instance struct {
	Name                             string
	Pinger                           bool
	PingInterval                     time.Duration
	Handler                          *peer.Peer
	Close                            chan bool
	Done                             chan bool
	RetryCounter                     int
	MaxRetries                       int
	Peers                            Peers
	OnCreate                         func()
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
	OnBridgeConnected                func(*Peer)
	OnRelayConnected                 func(*Peer)
	OnDiscoveryConnected             func(*Peer)
	isReconnecting                   bool
	mu                               sync.Mutex
	active_time_start                time.Time
	peerjs_config                    peer.Options
}

type PeerState struct {
	Uptime          string `json:"uptime"`
	ConnectionState bool   `json:"connection_state"`
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
