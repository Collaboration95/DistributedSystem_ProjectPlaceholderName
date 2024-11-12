package main

func main() {
	// Initialize the seats
	// InitSeats()
	// Start the server in a separate goroutine
	go Server_Session()
	StartClient()

	// Start handling requests (e.g., MonitorLockRequestRelease() is likely a long-running task)
	go MonitorLockRequestRelease()

	// Wait for the server to exit
	select {} // Server runs indefinitely, handling incoming requests
}
