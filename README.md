![banner](https://github.com/user-attachments/assets/ce3a4bd8-13aa-4e3c-ba99-75ed8e6a753f)

# Duplex
A minimal, Go-based library to run CL∆ clients or servers.

# Example Usage
```go
package main

import (
    // ...
    "github.com/cloudlink-delta/duplex"
)

func main() {

    // Creates a new duplex instance with a unique name.
    instance := duplex.New("my server")

    // Overrides the default behavior of a built-in CL Delta opcode.
    // Remapped functions have the highest priority during processing.
    instance.Remap("G_MSG", func(peer *duplex.Peer, packet *duplex.RxPacket) {
		// ...
	})

    // If you want to switch back to the built-in behavior of a opcode...
    instance.Unmap("G_MSG")

    // Creates a new opcode handler.
    // Custom handlers have the lowest priority during processing.
    // Any remapped or built-in handlers will run first.
    instance.Bind("MY_OPCODE", func(peer *duplex.Peer, packet *duplex.RxPacket) {
		// ...
	})

    // If you need to remove a custom handler...
    instance.Unbind("MY_OPCODE")

    // To run the instance, simply call Run().
    // Note that Run() will block code execution.
    instance.Run()
}
```