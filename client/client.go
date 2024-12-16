package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"math"
	"net/rpc"
	"os"
	"rpc-system/common"
	"strings"
	"sync"
	"time"

	"golang.org/x/exp/rand"
)

type Client struct {
	ID            string
	ServerID      string
	RPCClient     *rpc.Client
	RequestCh     chan common.Request
	HeartbeatDone chan struct{}
	WaitGroup     sync.WaitGroup
	LeaderPort    string
	rpcHandle     string
}

const (
	HeartbeatInterval = 10 * time.Second
)

func init_Client(clientID string, rpcClient *rpc.Client, rpcHandle string) *Client {
	serverID := fmt.Sprintf("server-session-%s", clientID)
	return &Client{
		ID:            clientID,
		ServerID:      serverID,
		RPCClient:     rpcClient,
		RequestCh:     make(chan common.Request),
		HeartbeatDone: make(chan struct{}),
		rpcHandle:     rpcHandle,
	}
}

func (c *Client) clientSession() {
	defer c.WaitGroup.Done()
	for req := range c.RequestCh {
		req.ClientID = c.ID
		req.ServerID = c.ServerID
		log.Printf("[Client %s] Sending request: %+v", c.ID, req)
		var res common.Response

		err := c.RPCClient.Call(fmt.Sprintf("%s.ProcessRequest", c.rpcHandle), &req, &res)
		if err != nil {
			log.Printf("[Client %s] Error sending request: %s", c.ID, err)
			continue
		}

		// Check if redirection is needed
		if res.Status == "REDIRECTINFORMATION" {
			log.Printf("[Client %s] Received redirect information: %s", c.ID, res.Message)
			// Reconnect to the new leader
			newRPCClient, newRpcHandle, err := connectToMasterServer()
			if err != nil {
				log.Printf("[Client %s] Failed to reconnect to new leader: %s", c.ID, err)
				continue
			}
			c.RPCClient = newRPCClient
			c.rpcHandle = newRpcHandle
			log.Printf("[Client %s] Reconnected to new leader", c.ID)
			continue
		}

		log.Printf("[Client %s] Server response (%s): %s", c.ID, res.Status, res.Message)
	}
	log.Printf("[Client %s] Request channel closed. Ending session.", c.ID)
}
func (c *Client) sendHeartbeat() {
	defer c.WaitGroup.Done()

	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			req := common.Request{
				ClientID: c.ID,
				ServerID: c.ServerID,
				Type:     "KEEPALIVE",
			}
			var res common.Response

			err := c.RPCClient.Call(fmt.Sprintf("%s.ProcessRequest", c.rpcHandle), &req, &res)
			if err != nil {
				log.Printf("[Client %s] Error sending KeepAlive: %s", c.ID, err)
				log.Println("[Client %s] Retrying connection...", c.ID)

				// Retry logic (e.g., reconnect to server or load balancer)
				c.RPCClient, c.rpcHandle, err = connectToMasterServer()
				if err != nil {
					log.Printf("[Client %s] Failed to reconnect. Exiting KeepAlive...", c.ID)
					return // Exit heartbeat loop
				}
				continue
			}

			// Handle redirect if received
			if res.Status == "REDIRECTINFORMATION" {
				log.Printf("[Client %s] Redirect information received: %s", c.ID, res.Message)
				newRPCClient, newRpcHandle, err := connectToMasterServer()
				if err != nil {
					log.Printf("[Client %s] Failed to reconnect to new leader: %s", c.ID, err)
					return
				}
				c.RPCClient = newRPCClient
				c.rpcHandle = newRpcHandle
				log.Printf("[Client %s] Reconnected to new leader", c.ID)
				continue
			}

			log.Printf("[Client %s] KeepAlive response: %s", c.ID, res.Message)

		case <-c.HeartbeatDone:
			log.Printf("[Client %s] Stopping KeepAlive.", c.ID)
			return
		}
	}
}

func connectToLoadBalancer(lbAddress string) (string, string, error) {
	const (
		maxRetries     = 15
		initialBackoff = 500 * time.Millisecond
		maxBackoff     = 15 * time.Second
	)

	var backoff time.Duration = initialBackoff
	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Dial the load balancer
		lbClient, err := rpc.Dial("tcp", lbAddress)
		if err != nil {
			return "", "", fmt.Errorf("failed to connect to load balancer: %w", err)
		}

		// Prepare request and response
		req := &common.Request{}
		res := &common.Response{}

		// Call LoadBalancer.GetLeaderIP
		err = lbClient.Call("LoadBalancer.GetLeaderIP", req, res)
		lbClient.Close() // Always close the client after the call

		if err != nil {
			return "", "", fmt.Errorf("failed to call GetLeaderIP on load balancer: %w", err)
		}

		// Check if the response indicates success
		if res.Status == "SUCCESS" {
			// Split the response to get port and ID
			parts := strings.Split(res.Message, ",")
			if len(parts) == 2 && parts[0] != "" && parts[1] != "" { // Ensure the response has valid data
				return parts[0], parts[1], nil
			}
		}

		// Log a warning and back off before retrying
		fmt.Printf("Attempt %d: Failed to get valid leader info (Response: %s). Retrying in %v...\n", attempt, res.Message, backoff)
		time.Sleep(backoff)

		// Increment the backoff duration (exponential growth with jitter)
		backoff = time.Duration(math.Min(float64(maxBackoff), float64(backoff)*1.5)) + time.Duration(rand.Intn(100))*time.Millisecond
	}

	return "", "", fmt.Errorf("exceeded max retries to connect to load balancer")
}

func connectToMasterServer() (*rpc.Client, string, error) {

	// First, connect to the load balancer
	leaderAddress, leaderID, err := connectToLoadBalancer("127.0.0.1:12345")
	if err != nil {
		return nil, "", fmt.Errorf("error getting leader from load balancer: %w", err)
	}
	fmt.Println("Leader Address is ", leaderAddress)
	fmt.Println("Leader ID is ", leaderID)

	// Now dial the leader server returned by the load balancer
	rpcClient, err := rpc.Dial("tcp", leaderAddress)
	if err != nil {
		return nil, "", fmt.Errorf("error connecting to leader server %s: %w", leaderAddress, err)
	}

	return rpcClient, leaderID, nil
}

func (c *Client) StartSession() {
	var reply common.Response
	err := c.RPCClient.Call(fmt.Sprintf("%s.CreateSession", c.rpcHandle), c.ID, &reply)
	if err != nil {
		log.Fatalf("[Client %s] Error creating session: %s", c.ID, err)
	}
	log.Printf("[Client %s] Session created: %s", c.ID, reply.Message)
	log.Printf("[Client %s] Raw Data Received: %+v", c.ID, reply.Data)
	if seats, ok := reply.Data.([]interface{}); ok {
		seatMap := make(map[string]map[string]string) // Map to group seats by rows and columns
		rows := []string{"1", "2", "3", "4", "5"}     // Define row numbers
		cols := []string{"A", "B", "C"}               // Define seat columns

		// Initialize all seats as blocked
		for _, row := range rows {
			seatMap[row] = make(map[string]string)
			for _, col := range cols {
				seatMap[row][col] = "[X]" // Blocked by default
			}
		}

		// Update map for available seats
		for _, seat := range seats {
			if seatStr, ok := seat.(string); ok {
				if len(seatStr) >= 2 { // Ensure seat ID is valid (e.g., "1A")
					row := string(seatStr[0]) // Extract row (e.g., "1" from "1A")
					col := string(seatStr[1]) // Extract column (e.g., "A" from "1A")
					if _, exists := seatMap[row]; exists {
						seatMap[row][col] = seatStr // Mark seat as available
					}
				}
			}
		}

		// Print row-wise seats
		fmt.Println("Available Seats:")
		for _, row := range rows {
			fmt.Printf("Row %s: ", row)
			for _, col := range cols {
				fmt.Print(seatMap[row][col], " ") // Display seat or blocked square
			}
			fmt.Println() // Move to the next row
		}
	} else if seats, ok := reply.Data.([]string); ok { // Handle direct []string
		seatMap := make(map[string]map[string]string) // Map to group seats by rows and columns
		rows := []string{"1", "2", "3", "4"}          // Define row numbers
		cols := []string{"A", "B", "C"}               // Define seat columns

		// Initialize all seats as blocked
		for _, row := range rows {
			seatMap[row] = make(map[string]string)
			for _, col := range cols {
				seatMap[row][col] = "[X]" // Blocked by default
			}
		}

		// Update map for available seats
		for _, seat := range seats {
			if len(seat) >= 2 { // Ensure seat ID is valid (e.g., "1A")
				row := string(seat[0]) // Extract row (e.g., "1" from "1A")
				col := string(seat[1]) // Extract column (e.g., "A" from "1A")
				if _, exists := seatMap[row]; exists {
					seatMap[row][col] = seat // Mark seat as available
				}
			}
		}

		// Print row-wise seats
		fmt.Println("Available Seats:")
		for _, row := range rows {
			fmt.Printf("Row %s: ", row)
			for _, col := range cols {
				fmt.Print(seatMap[row][col], " ") // Display seat or blocked square
			}
			fmt.Println() // Move to the next row
		}
	} else {
		fmt.Println("No seats available or invalid response data.")
	}

	c.WaitGroup.Add(2)
	go c.clientSession()
	go c.sendHeartbeat()

	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Enter your requests (Type 'done' to finish):")
	for {
		fmt.Print("Enter SeatID (e.g., 1A): \n")
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
		c.RequestCh <- request
	}

	close(c.RequestCh)     // Close the request channel after input is done
	close(c.HeartbeatDone) // Stop the heartbeat goroutine
	c.WaitGroup.Wait()     // Wait for both goroutines to finish
	fmt.Printf("Session ended for client: %s\n", c.ID)
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
	rpc_Client, rpcHandle, err := connectToMasterServer()

	if err != nil {
		log.Fatalf("Error connecting to server: %s", err)
	}
	defer rpc_Client.Close()

	client := init_Client(clientID, rpc_Client, rpcHandle)
	client.StartSession()

}
