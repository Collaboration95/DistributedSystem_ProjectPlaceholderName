package main

import (
	"log"
	"os"
)

func main() {
	// Initialize the seats
	// InitSeats()
	Server_Session()
	StartClient()

	// Create a logger for the server
	logger := log.New(os.Stdout, "[server] ", log.LstdFlags)

	// Log server startup message
	logger.Println("Server started")

	// Start handling requests (e.g., MonitorLockRequestRelease() is likely a long-running task)
	go MonitorLockRequestRelease()

	// Wait for the server to exit
	select {} // Server runs indefinitely, handling incoming requests
}
