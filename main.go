package main

import (
	"time"
)

func main() {
	// Start the server in a separate goroutine
	go Server_Session()

	// Give the server time to initialize
	time.Sleep(2 * time.Second)

	// Start the client to interact with the server
	go StartClient("client_requests.txt")

	// Wait for termination signal to gracefully shut down
	select {}
}
