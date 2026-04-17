package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cloudlink-delta/duplex"
)

func main() {

	// Setup
	fmt.Print("Enter a peer ID that you want to use: ")
	var name string
	_, err := fmt.Scanln(&name)
	if err != nil {
		fmt.Println("Error reading input:", err)
		return
	}

	// Spawn instance
	i := duplex.New(name, nil)

	// Graceful shutdown handler
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		i.Close <- true
		<-i.Done
		os.Exit(1)
	}()

	// Runner
	i.Run()
}
