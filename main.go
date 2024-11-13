package main

import (
	"fmt"
	"log"
	"strconv"
	"testProject/api"
	"time"
)

func checkError(err error) {
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
}

// WaitForElectionCompletion waits until a leader is elected for at least one server
func WaitForElectionCompletion(servers []*Server) {
	for {
		var leaderServer *Server
		for _, server := range servers {
			if server.RaftNode.State == Leader {
				leaderServer = server
				break
			}
		}
		if leaderServer != nil {
			status := leaderServer.GetStatus()
			fmt.Printf("Election completed: Server %d\n", status.ID)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func main() {
	servers := make([]*Server, serverCount)

	go api.StartServer(port)

	// Initialize and start servers
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
		intPort, _ := strconv.Atoi(port)
		finalPort := intPort + server.ID
		go StartRPCServer(fmt.Sprintf("%d", finalPort), server.LockSvc)
	}

	go LogAllServers(servers)
	go LogAllServersToJSON(servers, "./data/logs.json")

	// Start running servers
	for _, server := range servers {
		go server.Run()
	}

	// Wait for the election process to complete
	WaitForElectionCompletion(servers)

	// Initialize client and attempt lock requests only after leader is elected
	client := &Client{clientID: 1}
	session := &Session{}
	serverAddresses := []string{fmt.Sprintf("localhost:%s", port)}

	// Initialize client session and connect to a server
	err := session.InitSession(client.clientID, serverAddresses)
	checkError(err)
	client.chubbySession = session

	// Test RequestLock
	seatID := "seat-1"
	fmt.Println("Requesting lock for seat:", seatID)
	err = client.chubbySession.RequestLock(seatID)
	if err != nil {
		fmt.Printf("Failed to acquire lock: %v\n", err)
	} else {
		fmt.Printf("Lock acquired successfully for seat: %s\n", seatID)
	}

	// Test ReleaseLock
	fmt.Println("Releasing lock for seat:", seatID)
	err = client.chubbySession.ReleaseLock(seatID)
	if err != nil {
		fmt.Printf("Failed to release lock: %v\n", err)
	} else {
		fmt.Printf("Lock released successfully for seat: %s\n", seatID)
	}

	// Simulate running for some time
	time.Sleep(ProgramTimeOut * time.Second)

	// Stop servers after timeout
	fmt.Printf("Stopping Servers... after timeout of %d seconds\n", ProgramTimeOut)
	for _, server := range servers {
		close(server.QuitChan)
		close(server.RaftNode.QuitChan)
	}

	fmt.Println("Simulation ended.")
}
