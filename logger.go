package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Global interval for periodic logging
const logInterval = 10 * time.Second // OLD VALUE = 2s

// Struct to hold loggable status
type ServerStatus struct {
	ID          int
	State       ServerState
	CurrentTerm int
	VoteCount   int
	IsLeader    bool
}

func (s *Server) GetStatus() ServerStatus {
	isLeader := s.RaftNode.State == Leader
	return ServerStatus{
		ID:          s.ID,
		State:       s.RaftNode.State,
		CurrentTerm: s.RaftNode.CurrentTerm,
		VoteCount:   s.RaftNode.VoteCount,
		IsLeader:    isLeader,
	}
}

// Central logger to print status of all servers
func LogAllServers(servers []*Server) {
	for {
		fmt.Println("----- Cluster Status -----")
		for _, server := range servers {
			status := server.GetStatus()
			fmt.Printf("Server %d | State: %v | Term: %d | Votes: %d | Leader: %v\n",
				status.ID, status.State, status.CurrentTerm, status.VoteCount, status.IsLeader)
		}
		fmt.Println("--------------------------")
		time.Sleep(logInterval)
	}
}

// LogAllServersToJSON writes the status of all servers to a JSON file
func LogAllServersToJSON(servers []*Server, filename string) {
	for {
		statuses := make([]ServerStatus, 0, len(servers))
		for _, server := range servers {
			status := server.GetStatus()
			statuses = append(statuses, status)
		}

		// Create a data structure that includes the timestamp and statuses
		dataMap := map[string]interface{}{
			"timestamp": time.Now().Format("2006-01-02 15:04:05"),
			"status":    statuses,
		}

		// Convert dataMap to JSON
		data, err := json.MarshalIndent(dataMap, "", "  ")
		if err != nil {
			fmt.Println("Error marshalling JSON:", err)
			continue
		}

		// Write to file
		err = os.WriteFile(filename, data, 0644)
		if err != nil {
			fmt.Println("Error writing JSON to file:", err)
		}
		time.Sleep(logInterval)
	}
}
