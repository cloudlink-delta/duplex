package duplex

import (
	"fmt"
	"time"

	"github.com/goccy/go-json"
)

// Goroutine that writes messages to the peer.
func (c *Peer) Write(packet *TxPacket) {
	resp, err := json.Marshal(packet)
	if err != nil {
		c.Logger.Error().Err(err).Msg("failed to marshal packet for writing")
		return
	}

	c.Lock.Lock()
	defer c.Lock.Unlock()
	c.Logger.Debug().Str("direction", "out").RawJSON("packet", []byte(packet.String())).Msg("sending packet")
	c.Send(resp, true)
}

// WriteBlocking is a variant of Write that has a blocking mode that exits when
// it has finished sending the entire message to the recipient.
func (c *Peer) WriteBlocking(packet *TxPacket) {
	resp, err := json.Marshal(packet)
	if err != nil {
		c.Logger.Error().Err(err).Msg("failed to marshal packet for writing")
		return
	}

	c.Lock.Lock()
	c.Send(resp, true)
	c.Lock.Unlock()

	// Wait until the buffer is flushed (the message is fully sent)
	if dc := c.DataChannel; dc != nil {
		for dc.BufferedAmount() > 0 {
			select {
			case <-c.Done:
				return
			default:
				time.Sleep(time.Millisecond)
			}
		}
	}
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
			c.Logger.Error().Err(err).Str("type", fmt.Sprintf("%T", v)).Msg("Unsupported data type and failed to marshal")
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
		c.Logger.Error().Err(err).RawJSON("raw_packet", raw).Msg("Error unmarshaling inner packet")
		return nil
	}

	return &packet
}

// Returns the peer's preferred ID.
func (c *Peer) GiveName() string {
	if c.GiveNameRemapper != nil {
		return c.GiveNameRemapper()
	}
	return fmt.Sprintf("[%s]", c.GetPeerID())
}

// Returns true if the peer does not advertise any features.
func (c *Peer) IsClient() bool {
	return !c.IsRelay &&
		!c.IsDiscovery &&
		!c.IsBridge
}
