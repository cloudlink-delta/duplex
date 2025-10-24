package duplex

import (
	"fmt"
	"log"

	"github.com/goccy/go-json"
)

func (c *Peer) Write(packet *TxPacket) {
	resp, err := json.Marshal(packet)
	if err != nil {
		log.Println(err)
		return
	}

	c.Lock.Lock()
	defer c.Lock.Unlock()
	log.Printf("%s 🢀 %v", c.GiveName(), packet)
	c.Send(resp, true)
}

func (c *Peer) Read(data any) *RxPacket {

	// 1. First Unmarshal: Decode the outer JSON string into a Go string
	var jsonPayload string
	if err := json.Unmarshal(data.([]uint8), &jsonPayload); err != nil {
		log.Println("Error unmarshaling outer layer:", err)
		return nil
	}

	// 2. Second Unmarshal: Parse the actual payload string into the RxPacket struct
	var packet RxPacket
	if err := json.Unmarshal([]byte(jsonPayload), &packet); err != nil {
		log.Println("Error unmarshaling inner packet:", err)
		return nil
	}

	return &packet
}

func (c *Peer) GiveName() string {
	return fmt.Sprintf("[%s]", c.GetPeerID())
}
