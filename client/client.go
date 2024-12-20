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
	rpcHandle     string
}

const (
	HeartbeatInterval = 8 * time.Second
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
		lbClient, err := rpc.Dial("tcp", lbAddress)
		if err != nil {
			return "", "", fmt.Errorf("failed to connect to load balancer: %w", err)
		}

		req := &common.Request{}
		res := &common.Response{}

		err = lbClient.Call("LoadBalancer.GetLeaderIP", req, res)
		lbClient.Close()

		if err != nil {
			return "", "", fmt.Errorf("failed to call GetLeaderIP on load balancer: %w", err)
		}

		if res.Status == "SUCCESS" {
			parts := strings.Split(res.Message, ",")
			if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
				return parts[0], parts[1], nil
			}
		}

		fmt.Printf("Attempt %d: Failed to get valid leader info (Response: %s). Retrying in %v...\n", attempt, res.Message, backoff)
		time.Sleep(backoff)
		backoff = time.Duration(math.Min(float64(maxBackoff), float64(backoff)*1.5)) + time.Duration(rand.Intn(100))*time.Millisecond
	}

	return "", "", fmt.Errorf("exceeded max retries to connect to load balancer")
}

func connectToMasterServer() (*rpc.Client, string, error) {
	leaderAddress, leaderID, err := connectToLoadBalancer("127.0.0.1:12345")
	if err != nil {
		return nil, "", fmt.Errorf("error getting leader from load balancer: %w", err)
	}
	fmt.Println("Leader Address is ", leaderAddress)
	fmt.Println("Leader ID is ", leaderID)

	rpcClient, err := rpc.Dial("tcp", leaderAddress)
	if err != nil {
		return nil, "", fmt.Errorf("error connecting to leader server %s: %w", leaderAddress, err)
	}

	return rpcClient, leaderID, nil
}

func (c *Client) HandleRequest(request common.Request) {
	fmt.Printf("[Client %s] Handling request: %+v\n", c.ID, request)
	c.RequestCh <- request
}

func (c *Client) StartSession() error {
	var reply common.Response
	err := c.RPCClient.Call(fmt.Sprintf("%s.CreateSession", c.rpcHandle), c.ID, &reply)
	if err != nil {
		return fmt.Errorf("[Client %s] Error creating session: %s", c.ID, err)
	}
	log.Printf("[Client %s] Session created: %s", c.ID, reply.Message)
	log.Printf("[Client %s] Raw Data Received: %+v", c.ID, reply.Data)

	// Display the seat map
	c.displaySeatMap(reply.Data)

	c.WaitGroup.Add(2)
	go c.clientSession()
	go c.sendHeartbeat()

	return nil
}

func (c *Client) displaySeatMap(data interface{}) {
	switch seats := data.(type) {
	case []interface{}:
		seatMap := make(map[string]map[string]string)
		rows := []string{"1", "2", "3", "4", "5"}
		cols := []string{"A", "B", "C"}

		// Initialize all seats as blocked
		for _, row := range rows {
			seatMap[row] = make(map[string]string)
			for _, col := range cols {
				seatMap[row][col] = "[X]"
			}
		}

		// Mark available seats
		for _, seat := range seats {
			if seatStr, ok := seat.(string); ok && len(seatStr) >= 2 {
				row := string(seatStr[0])
				col := string(seatStr[1])
				if _, exists := seatMap[row]; exists {
					seatMap[row][col] = seatStr
				}
			}
		}

		fmt.Println("Available Seats:")
		for _, row := range rows {
			fmt.Printf("Row %s: ", row)
			for _, col := range cols {
				fmt.Print(seatMap[row][col], " ")
			}
			fmt.Println()
		}

	case []string:
		seatMap := make(map[string]map[string]string)
		rows := []string{"1", "2", "3", "4"}
		cols := []string{"A", "B", "C"}

		for _, row := range rows {
			seatMap[row] = make(map[string]string)
			for _, col := range cols {
				seatMap[row][col] = "[X]"
			}
		}

		for _, seat := range seats {
			if len(seat) >= 2 {
				row := string(seat[0])
				col := string(seat[1])
				if _, exists := seatMap[row]; exists {
					seatMap[row][col] = seat
				}
			}
		}

		fmt.Println("Available Seats:")
		for _, row := range rows {
			fmt.Printf("Row %s: ", row)
			for _, col := range cols {
				fmt.Print(seatMap[row][col], " ")
			}
			fmt.Println()
		}
	default:
		fmt.Println("No seats available or invalid response data.")
	}
}

func main() {
	clientIDs := []string{"client1", "client2", "client3"}

	clients := make(map[string]*Client)
	for _, cid := range clientIDs {
		rpc_Client, rpcHandle, err := connectToMasterServer()
		if err != nil {
			log.Fatalf("Error connecting client %s to server: %s", cid, err)
		}
		client := init_Client(cid, rpc_Client, rpcHandle)
		err = client.StartSession()
		if err != nil {
			log.Fatalf("Error starting session for client %s: %s", cid, err)
		}
		clients[cid] = client
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Enter commands as: clientID SeatID RequestType. Type 'done' to exit.")

	for {
		fmt.Print("Enter your request: ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if strings.ToLower(input) == "done" {
			fmt.Println("Finishing input. Closing all request channels...")
			break
		}

		parts := strings.Fields(input)
		if len(parts) != 3 {
			fmt.Println("Invalid input. Format: clientID SeatID RequestType")
			continue
		}

		clientID := parts[0]
		seatID := parts[1]
		reqType := parts[2]

		client, exists := clients[clientID]
		if !exists {
			fmt.Printf("No such client: %s\n", clientID)
			continue
		}

		request := common.Request{
			SeatID: seatID,
			Type:   reqType,
		}
		client.HandleRequest(request)
	}

	// User typed 'done', now shut down all clients
	for _, client := range clients {
		close(client.RequestCh)
		close(client.HeartbeatDone)
	}

	// Wait for all clients to finish
	for _, client := range clients {
		client.WaitGroup.Wait()
		fmt.Printf("Session ended for client: %s\n", client.ID)
	}
}

func getClientID() string {
	clientID := flag.String("clientID", "", "Unique client ID")
	flag.Parse()
	return *clientID
}
