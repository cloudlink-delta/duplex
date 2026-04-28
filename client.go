package duplex

import (
	"bytes"
	"fmt"
	"time"
	"unicode/utf8"

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
		// If it's already a struct/map, we trust it came from internal code
		b, err := json.Marshal(v)
		if err != nil {
			c.Logger.Error().Err(err).Str("type", fmt.Sprintf("%T", v)).Msg("Unsupported data type")
			return nil
		}
		raw = b
	}

	// Hard Size Limit
	// PeerJS signaling packets (SDP/ICE) are almost never > 64KB.
	// This helps mitigate memory exhaustion before parsing.
	if len(raw) > 1024*64 {
		c.Logger.Warn().Int("size", len(raw)).Msg("Rejected oversized packet")
		return nil
	}

	// Fast UTF-8 Validation
	// Validating UTF-8 is significantly faster than Unmarshaling JSON.
	if !utf8.Valid(raw) {
		c.Logger.Warn().Msg("Rejected binary frame (invalid UTF-8)")
		return nil
	}

	// 3. Structural "First-Byte" Check
	// PeerJS packets must be a JSON Object (starts with '{')
	// or a double-encoded JSON String (starts with '"').
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '"') {
		c.Logger.Warn().Msg("Rejected packet: Not a valid JSON object or string")
		return nil
	}

	// Attempt to decode as a JSON string (Double Encoding)
	if trimmed[0] == '"' {
		var jsonString string
		if err := json.Unmarshal(raw, &jsonString); err == nil {
			raw = []byte(jsonString)
			// Re-verify the inner raw is an object
			trimmed = bytes.TrimSpace(raw)
			if len(trimmed) == 0 || trimmed[0] != '{' {
				return nil
			}
		}
	}

	// Decode into the RxPacket struct
	var packet RxPacket
	if err := json.Unmarshal(raw, &packet); err != nil {
		// Log error but don't include the raw data if it's too large
		// to prevent log-filling attacks
		c.Logger.Error().Msg("Error unmarshaling inner packet")
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
