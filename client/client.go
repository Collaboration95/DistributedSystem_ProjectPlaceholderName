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

type Client struct {
	ID            string
	ServerID      string
	RPCClient     *rpc.Client
	RequestCh     chan common.Request
	HeartbeatDone chan struct{}
	WaitGroup     sync.WaitGroup
	LeaderPort    string
}

const (
	HeartbeatInterval = 5 * time.Second
	KeepAliveTimeout  = 10 * time.Second
)

func init_Client(clientID string, rpcClient *rpc.Client) *Client {
	serverID := fmt.Sprintf("server-session-%s", clientID)
	return &Client{
		ID:            clientID,
		ServerID:      serverID,
		RPCClient:     rpcClient,
		RequestCh:     make(chan common.Request),
		HeartbeatDone: make(chan struct{}),
	}
}

func (c *Client) clientSession() {
	defer c.WaitGroup.Done()
	for req := range c.RequestCh {
		req.ClientID = c.ID
		req.ServerID = c.ServerID
		log.Printf("[Client %s] Sending request: %+v", c.ID, req)
		var res common.Response
		err := c.RPCClient.Call("Server.ProcessRequest", &req, &res)
		if err != nil {
			log.Printf("[Client %s] Error sending request: %s", c.ID, err)
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
			err := c.RPCClient.Call("Server.ProcessRequest", &req, &res)
			if err != nil {
				log.Printf("[Client %s] Error sending KeepAlive: %s", c.ID, err)
				return // Assume server is unreachable and exit
			}
			log.Printf("\n[Client %s] KeepAlive response: %s", c.ID, res.Message)

		case <-c.HeartbeatDone:
			log.Printf("[Client %s] Stopping KeepAlive.", c.ID)
			return
		}
	}
}

func connectToLoadBalancer(lbAddress string) (string, error) {
	// Dial the load balancer
	lbClient, err := rpc.Dial("tcp", lbAddress)
	if err != nil {
		return "", fmt.Errorf("failed to connect to load balancer: %w", err)
	}
	defer lbClient.Close()

	// Prepare request and response
	req := &common.Request{}
	res := &common.Response{}

	// Call LoadBalancer.GetLeaderIP
	err = lbClient.Call("LoadBalancer.GetLeaderIP", req, res)
	if err != nil {
		return "", fmt.Errorf("failed to call GetLeaderIP on load balancer: %w", err)
	}

	if res.Status != "SUCCESS" {
		return "", fmt.Errorf("load balancer failed to provide leader IP: %s", res.Message)
	}

	return res.Message, nil
}

func connectToMasterServer() (*rpc.Client, error) {
	// First, connect to the load balancer (which is running on :12345 in your example)
	leaderAddress, err := connectToLoadBalancer("127.0.0.1:12345")
	if err != nil {
		return nil, fmt.Errorf("error getting leader from load balancer: %w", err)
	}
	fmt.Println("=---------ß------Leader Address is ", leaderAddress)
	// Now dial the leader server returned by the load balancer
	rpcClient, err := rpc.Dial("tcp", leaderAddress)
	if err != nil {
		return nil, fmt.Errorf("error connecting to leader server %s: %w", leaderAddress, err)
	}

	return rpcClient, nil
}

func (c *Client) StartSession() {
	var reply string
	err := c.RPCClient.Call("Server.CreateSession", c.ID, &reply)
	if err != nil {
		log.Fatalf("[Client %s] Error creating session: %s", c.ID, err)
	}
	log.Printf("[Client %s] Session created: %s", c.ID, reply)
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
	rpc_Client, err := connectToMasterServer()

	if err != nil {
		log.Fatalf("Error connecting to server: %s", err)
	}
	defer rpc_Client.Close()

	// Start the client session
	// startClientSession(clientID, rpc_Client)
	client := init_Client(clientID, rpc_Client)
	client.StartSession()

}
