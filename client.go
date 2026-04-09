package duplex

import (
	"fmt"
	"log"

	"github.com/goccy/go-json"
)

// Goroutine that writes messages to the peer.
func (c *Peer) Write(packet *TxPacket) {
	resp, err := json.Marshal(packet)
	if err != nil {
		log.Println(err)
		return
	}

	c.Lock.Lock()
	defer c.Lock.Unlock()
	log.Printf("%s 🢀  %v", c.GiveName(), packet)
	c.Send(resp, true)
}

// Goroutine that reads incoming messages from the peer.
func (c *Peer) Read(data any) *RxPacket {
	var raw []byte
	switch v := data.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			log.Printf("Unsupported data type %T and failed to marshal: %v", v, err)
			return nil
		}
		raw = b
	}

	// 1. Attempt to decode as a JSON string (e.g. if sent from JS client with double encoding)
	var jsonString string
	if err := json.Unmarshal(raw, &jsonString); err == nil {
		// It was a JSON string, so update raw to the inner JSON
		raw = []byte(jsonString)
	}

	// 2. Decode into the RxPacket struct
	var packet RxPacket
	if err := json.Unmarshal(raw, &packet); err != nil {
		log.Println("Error unmarshaling inner packet:", err)
		return nil
	}

	return &packet
}

// Returns the peer's preferred ID.
func (c *Peer) GiveName() string {
	return fmt.Sprintf("[%s]", c.GetPeerID())
}

// Returns true if the peer does not advertise any features.
func (c *Peer) IsClient() bool {
	return !c.IsRelay &&
		!c.IsDiscovery &&
		!c.IsBridge
}
