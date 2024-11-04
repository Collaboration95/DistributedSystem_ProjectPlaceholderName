package main

import (
	"fmt"
	"testProject/api"
	"time"
)

func main() {
	servers := make([]*Server, serverCount)

	go api.StartServer(port)

	for i := 0; i < serverCount; i++ {
		servers[i] = &Server{
			ID:       i,
			QuitChan: make(chan bool),
		}
	}

	// Set peers and initialize RaftNode and LockService
	for _, server := range servers {
		server.Peers = make([]*Server, 0)
		for _, peer := range servers {
			if peer.ID != server.ID {
				server.Peers = append(server.Peers, peer)
			}
		}
		server.RaftNode = NewRaftNode(server)
		server.LockSvc = NewLockService()
	}

	go LogAllServers(servers)
	go LogAllServersToJSON(servers, "./data/logs.json")

	// Run servers
	for _, server := range servers {
		go server.Run()
	}

	// Simulate running

	time.Sleep(ProgramTimeOut * time.Second)

	fmt.Printf("Stopping Servers... after timeout of %d seconds\n", ProgramTimeOut)
	for _, server := range servers {
		close(server.QuitChan)
		close(server.RaftNode.QuitChan)
	}

	fmt.Println("Simulation ended.")
}
