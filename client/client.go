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
)

// clientSession continuously processes requests from a channel until it is closed.
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
		log.Printf("[Client %s] Response from server: %s", clientID, res.Message)
	}

	log.Printf("[Client %s] Request channel closed. Ending session.", clientID)
}

// connectToMasterServer establishes an RPC connection to the server.
func connectToMasterServer() (*rpc.Client, error) {
	client, err := rpc.Dial("tcp", "127.0.0.1:12345") // Connect to server
	if err != nil {
		return nil, err
	}
	return client, nil
}

// startClientSession starts the client session and allows dynamic input of requests.
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
	wg.Add(1)

	// Start the client session as a goroutine
	go clientSession(clientID, serverID, rpcClient, requestCh, &wg)

	// Input loop for dynamic request entry
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Enter your requests (Type 'done' to finish):")

	for {
		// Prompt for SeatID
		fmt.Print("Enter SeatID (e.g., 1A): ")
		seatID, _ := reader.ReadString('\n')
		seatID = strings.TrimSpace(seatID)

		if strings.ToLower(seatID) == "done" { // Exit input loop
			fmt.Println("Finishing input. Closing request channel...")
			break
		}

		// Prompt for Request Type
		fmt.Print("Enter Request Type (e.g., RESERVE or CANCEL): ")
		reqType, _ := reader.ReadString('\n')
		reqType = strings.TrimSpace(reqType)

		if seatID == "" || reqType == "" { // Validate input
			fmt.Println("Invalid input. Please enter both SeatID and Request Type.")
			continue
		}

		// Send the request to the channel
		request := common.Request{
			SeatID: seatID,
			Type:   reqType,
		}
		fmt.Printf("Added request to queue: %+v\n", request) // Explicit prompt without logging
		requestCh <- request
	}

	// Close the channel after user input is done
	close(requestCh)
	wg.Wait() // Wait for the session goroutine to finish
	fmt.Printf("Session ended for client: %s\n", clientID)
}

// getClientID parses the client ID from command-line arguments.
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
