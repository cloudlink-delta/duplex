package duplex

import (
	"log"
	"slices"
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

		// Verify if the remapped handler requires any special features
		if required_features := conn.Parent.RemappedHandlersRequiredFeatures[r.Opcode]; len(required_features) > 0 {

			// Check if the peer has all the required features
			for _, feature := range required_features {
				if slices.Contains(conn.Features, feature) {
					log.Printf("%s dropped packet \"%s\": missing required feature %s", conn.GiveName(), r.Opcode, feature)
					return
				}
			}
		}

		remapped(conn, r)
		return
	}

	// Listener handlers take second priority
	if listener, ok := conn.Listeners[r.Listener]; ok {
		listener(r)
		return
	}

	// Matchers briefly take priority for individual connections
	if matcher, ok := conn.OpcodeMatchers[conn]; ok {
		if slices.Contains(matcher.Opcodes, r.Opcode) {
			matcher.Callback(r)
			return
		}
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

	case "PONG":

		type PongReply struct {
			T1 int64 `json:"t1"`
			T2 int64 `json:"t2"`
		}

		var reply PongReply
		err := json.Unmarshal(r.Payload, &reply)
		if err != nil {
			log.Println(err)
			return
		}

		now := time.Now().UnixNano() / 1000000
		log.Printf("%s latency: %dms", conn.GiveName(), now-reply.T1)

	default:

		// Process custom opcodes if there are any
		if handler, ok := conn.Parent.CustomHandlers[r.Opcode]; ok {

			// Verify if the custom handler requires any special features
			if required_features := conn.Parent.CustomHandlersRequiredFeatures[r.Opcode]; len(required_features) > 0 {

				// Check if the peer has all the required features
				for _, feature := range required_features {
					if slices.Contains(conn.Features, feature) {
						log.Printf("%s dropped packet \"%s\": missing required feature %s", conn.GiveName(), r.Opcode, feature)
						return
					}
				}
			}

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
		conn.IsBridge = true
	}
	if arguments.IsRelay {
		advertised_features = append(advertised_features, "relay")
		conn.IsRelay = true
	}
	if arguments.IsDiscovery {
		advertised_features = append(advertised_features, "discovery")
		conn.IsDiscovery = true
	}

	if len(advertised_features) > 0 {
		log.Printf("%s advertises the following features: %v", conn.GiveName(), strings.Join(advertised_features, ", "))
	}

	// Store our advertised features
	conn.Features = advertised_features

	// Reply with our capabilities and version if we are the responder
	if !conn.IsInitiator {
		conn.SendNegotiate(reader)
	}

	// Run callbacks
	if fn := conn.Parent.AfterNegotiation; fn != nil {
		go fn(conn)
	}
	if fn := conn.Parent.OnBridgeConnected; fn != nil && arguments.IsBridge {
		go fn(conn)
	}
	if fn := conn.Parent.OnRelayConnected; fn != nil && arguments.IsRelay {
		go fn(conn)
	}
	if fn := conn.Parent.OnDiscoveryConnected; fn != nil && arguments.IsDiscovery {
		go fn(conn)
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

// SendAndWaitForReply sends a packet and waits for a response with the given opcode.
// The packet needs to be tagged with a listener string. The function will return a
// channel that will receive the response packet when it is received.
// If the opcode of the received packet does not match the reply opcode, the function will
// return nil.
// The function will also return nil if the packet is not tagged with a listener string.
// The function will block until the response is received or the underlying connection is closed.
// The function is safe for concurrent use.
func (conn *Peer) SendAndWaitForReply(request *TxPacket) *RxPacket {

	// Needs to be tagged with a listener
	if request.Listener == "" {
		return nil
	}

	// Create response channel
	response := make(chan *RxPacket, 1)

	// Create a callback function
	listener_func := func(r *RxPacket) {

		// Unbind the listener
		delete(conn.Listeners, request.Listener)

		// Return the callback
		response <- r
	}

	// Bind the listener
	conn.Listeners[request.Listener] = listener_func

	// Send the packet
	go conn.Write(request)

	// Wait for the response
	return <-response
}

// Creates a callback that fires whenever a specific connection receives a specific packet opcode.
func (conn *Peer) WaitForMatchedPacket(opcodes ...string) *RxPacket {

	// Create response channel
	response := make(chan *RxPacket, 1)

	// Create a callback function
	listener_func := func(r *RxPacket) {

		// Unbind the listener
		delete(conn.OpcodeMatchers, conn)

		response <- r
	}

	conn.OpcodeMatchers[conn] = &OpcodeMatcher{
		Opcodes:  opcodes,
		Callback: listener_func,
	}

	// Now we will forever wait until the packet is received with the matched opcode

	return <-response
}
