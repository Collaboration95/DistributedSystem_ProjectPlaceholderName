package api

import (
	"fmt"
	"net/http"
)

// StartServer sets up the router and starts the HTTP server
func StartServer(port string) {
	mux := http.NewServeMux()

	// Register /logs endpoint
	mux.HandleFunc("/logs", ServeLogs)

	// Add more endpoints as needed, e.g., /status, /metrics, etc.
	// mux.HandleFunc("/status", ServeStatus)
	// mux.HandleFunc("/metrics", ServeMetrics)

	fmt.Printf("Server is running on http://localhost:%s\n", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		fmt.Println("Failed to start server:", err)
	}
}
