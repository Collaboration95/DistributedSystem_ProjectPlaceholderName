package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net/rpc"
	"os"
	"rpc-system/common"
	"strings"
	"sync"
	"time"
)

const (
	HeartbeatInterval = 5 * time.Second
	KeepAliveTimeout  = 10 * time.Second
)

// clientSession processes requests from the input channel and handles responses
func clientSession(clientID, serverID string, client *rpc.Client, requestCh chan common.Request, wg *sync.WaitGroup) {
	defer wg.Done()

	for req := range requestCh {
		req.ClientID = clientID
		req.ServerID = serverID

		log.Printf("[Client %s] Sending request: %+v", clientID, req)

		var res common.Response
		err := client.Call("Server.ProcessRequest", &req, &res)
		if err != nil {
			log.Printf("[Client %s] Error sending request: %s", clientID, err)
			continue
		}

		// Log the server response
		log.Printf("[Client %s] Server response (%s): %s", clientID, res.Status, res.Message)
	}

	log.Printf("[Client %s] Request channel closed. Ending session.", clientID)
}

// sendHeartbeat sends periodic keepalive messages to the server
func sendHeartbeat(clientID, serverID string, client *rpc.Client, done chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			req := common.Request{
				ClientID: clientID,
				ServerID: serverID,
				Type:     "KEEPALIVE",
			}

			var res common.Response
			err := client.Call("Server.ProcessRequest", &req, &res)
			if err != nil {
				log.Printf("[Client %s] Error sending KeepAlive: %s", clientID, err)
				return // Assume server is unreachable and exit
			}
			log.Printf("[Client %s] KeepAlive response: %s", clientID, res.Message)

		case <-done:
			log.Printf("[Client %s] Stopping KeepAlive.", clientID)
			return
		}
	}
}

// connectToMasterServer establishes an RPC connection to the server
func connectToMasterServer() (*rpc.Client, error) {

	client, err := rpc.Dial("tcp", "127.0.0.1:12345") // Connect to server
	if err != nil {
		return nil, err
	}
	return client, nil
}

// startClientSession starts the client session and manages dynamic request input and heartbeats
func startClientSession(clientID string, rpcClient *rpc.Client) {
	var reply string
	err := rpcClient.Call("Server.CreateSession", clientID, &reply)
	if err != nil {
		log.Fatalf("[Client %s] Error creating session: %s", clientID, err)
	}
	log.Printf("[Client %s] Session created: %s", clientID, reply)

	serverID := fmt.Sprintf("server-session-%s", clientID)

	// Create a channel for sending requests
	requestCh := make(chan common.Request)
	var wg sync.WaitGroup
	wg.Add(2)

	// Channel to signal the heartbeat goroutine to stop
	heartbeatDone := make(chan struct{})

	// Start the client session as a goroutine
	go clientSession(clientID, serverID, rpcClient, requestCh, &wg)

	// Start the heartbeat mechanism
	go sendHeartbeat(clientID, serverID, rpcClient, heartbeatDone, &wg)

	// Input loop for dynamic request entry
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Enter your requests (Type 'done' to finish):")

	for {
		fmt.Print("Enter SeatID (e.g., 1A): ")
		seatID, _ := reader.ReadString('\n')
		seatID = strings.TrimSpace(seatID)

		if strings.ToLower(seatID) == "done" { // Exit input loop
			fmt.Println("Finishing input. Closing request channel...")
			break
		}

		fmt.Print("Enter Request Type (e.g., RESERVE or CANCEL): ")
		reqType, _ := reader.ReadString('\n')
		reqType = strings.TrimSpace(reqType)

		if seatID == "" || reqType == "" {
			fmt.Println("Invalid input. Please enter both SeatID and Request Type.")
			continue
		}

		request := common.Request{
			SeatID: seatID,
			Type:   reqType,
		}
		fmt.Printf("Added request to queue: %+v\n", request)
		requestCh <- request
	}

	close(requestCh)     // Close the request channel after input is done
	close(heartbeatDone) // Stop the heartbeat goroutine
	wg.Wait()            // Wait for both goroutines to finish
	fmt.Printf("Session ended for client: %s\n", clientID)
}

// getClientID parses the client ID from command-line arguments
func getClientID() string {
	clientID := flag.String("clientID", "", "Unique client ID")
	flag.Parse()
	if *clientID == "" {
		log.Fatalf("Client ID is required. Use --clientID flag to specify one.")
	}
	return *clientID
}

func main() {
	// Get the client ID from the command line
	clientID := getClientID()

	// Connect to the server
	client, err := connectToMasterServer()

	if err != nil {
		log.Fatalf("Error connecting to server: %s", err)
	}
	defer client.Close()

	// Start the client session
	startClientSession(clientID, client)
}
