package api

import (
	"net/http"
	"os"
)

func ServeLogs(w http.ResponseWriter, r *http.Request) {
	// Read the log file
	data, err := os.ReadFile("./data/logs.json")
	if err != nil {
		http.Error(w, "Failed to read log file", http.StatusInternalServerError)
		return
	}

	// Set the content type to JSON
	w.Header().Set("Content-Type", "application/json")

	// Write the file contents to the response
	w.Write(data)
}
