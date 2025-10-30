package duplex

import (
	"log"
	"strings"
	"time"

	"github.com/goccy/go-json"
)

func (conn *Peer) HandlePacket(r *RxPacket) {

	// Decrement TTL
	r.TTL--

	// Drop packet if TTL is < 0
	if r.TTL < 0 {
		log.Printf("%s dropped packet \"%s\": TTL expired", conn.GiveName(), r.Opcode)
		return
	}

	// Remapped functions take precedence
	if remapped, ok := conn.Parent.RemappedHandlers[r.Opcode]; ok {
		remapped(conn, r)
		return
	}

	// Process builtin opcodes first
	switch r.Opcode {
	case "NEGOTIATE":
		conn.HandleNegotiate(r)

	case "PING":

		type PingRequest struct {
			T1 int64 `json:"t1"`
		}

		then := &PingRequest{}

		err := json.Unmarshal(r.Payload, &then)
		if err != nil {
			log.Println(err)
			return
		}

		var now = time.Now().UnixNano() / 1000000

		type PongReply struct {
			T1 int64 `json:"t1"`
			T2 int64 `json:"t2"`
		}

		conn.Write(&TxPacket{
			Packet: Packet{
				Opcode:   "PONG",
				TTL:      1,
				Listener: r.Listener,
			},
			Payload: PongReply{
				T1: then.T1,
				T2: now,
			},
		})

	default:

		// Process custom opcodes if there are any
		if handler, ok := conn.Parent.CustomHandlers[r.Opcode]; ok {
			handler(conn, r)
		}
	}
}

func (conn *Peer) HandleNegotiate(reader *RxPacket) {
	var arguments NegotiationArgs
	err := json.Unmarshal(reader.Payload, &arguments)
	if err != nil {
		log.Println(err)
		return
	}

	log.Printf("%s using dialect revision %d on %s (v%d.%d.%d)\n",
		conn.GiveName(),
		arguments.SpecVersion,
		arguments.Version.Type,
		arguments.Version.Major,
		arguments.Version.Minor,
		arguments.Version.Patch,
	)

	var advertised_features []string
	if arguments.IsBridge {
		advertised_features = append(advertised_features, "bridge")
		// TODO: request list of bridged peers and their CL versions/dialects for translation
	}
	if arguments.IsRelay {
		advertised_features = append(advertised_features, "relay")
		// TODO: reconfigure routes to use relay for broadcasts
	}
	if arguments.IsDiscovery {
		advertised_features = append(advertised_features, "discovery")
		// TODO: verify authenticity of discovery server
	}

	if len(advertised_features) > 0 {
		log.Printf("%s advertises the following features: %v", conn.GiveName(), strings.Join(advertised_features, ", "))
	}

	// Reply with our capabilities and version
	conn.SendNegotiate(reader)

	if fn := conn.Parent.AfterNegotiation; fn != nil {
		fn(conn)
	}
}

// SendNegotiate sends a NEGOTIATE packet to a newly connected peer.
func (conn *Peer) SendNegotiate(r *RxPacket) {
	conn.Write(&TxPacket{
		Packet: Packet{
			Opcode:   "NEGOTIATE",
			TTL:      1,
			Listener: r.Listener,
		},
		Payload: NegotiationArgs{
			Version: VersionArgs{
				Type:  "Go", // Do not change this
				Major: 1,
				Minor: 0,
				Patch: 1,
			},
			SpecVersion: 0,
			Plugins:     []string{},
			IsBridge:    conn.Parent.IsBridge,
			IsRelay:     conn.Parent.IsRelay,
			IsDiscovery: conn.Parent.IsDiscovery,
		},
	})
}
