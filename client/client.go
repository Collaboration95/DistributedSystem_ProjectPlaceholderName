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
	"strconv"
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

var wg sync.WaitGroup
var mu sync.Mutex
var wgCounter int

func decrementWaitGroup() {
	mu.Lock()
	defer mu.Unlock()
	if wgCounter > 0 {
		wg.Done()
		wgCounter--
	}
}

var seatMapPrinted = false

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

		start := time.Now()

		var res common.Response
		err := c.RPCClient.Call(fmt.Sprintf("%s.ProcessRequest", c.rpcHandle), &req, &res)
		elapsed := time.Since(start) // Measure how long the call took

		if err != nil {
			log.Printf("[Client %s] Error sending request: %s", c.ID, err)
			log.Printf("[Client %s] Request took: %s", c.ID, elapsed)
			continue
		}

		if res.Status == "REDIRECTINFORMATION" {
			log.Printf("[Client %s] Received redirect information: %s", c.ID, res.Message)
			// Reconnect to the new leader
			newRPCClient, newRpcHandle, err := connectToMasterServer()
			if err != nil {
				log.Printf("[Client %s] Failed to reconnect to new leader: %s", c.ID, err)
				log.Printf("[Client %s] Request took: %s", c.ID, elapsed)
				continue
			}
			c.RPCClient = newRPCClient
			c.rpcHandle = newRpcHandle
			log.Printf("[Client %s] Reconnected to new leader", c.ID)
			log.Printf("[Client %s] Request took: %s", c.ID, elapsed)
			continue
		}

		log.Printf("[Client %s] Server response (%s): %s", c.ID, res.Status, res.Message)
		decrementWaitGroup()
		log.Printf("[Client %s] Request took: %s", c.ID, elapsed)
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

	if !seatMapPrinted {
		c.displaySeatMap(reply.Data)
		seatMapPrinted = true
	}

	c.WaitGroup.Add(2)
	go c.clientSession()
	go c.sendHeartbeat()

	return nil
}

func (c *Client) displaySeatMap(data interface{}) {
	switch seats := data.(type) {
	case []interface{}:
		seatMap := make(map[string]map[string]string)
		rows := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15", "16", "17", "18", "19", "20", "21", "22", "23", "24", "25"}
		cols := []string{"A", "B", "C", "D", "E", "F", "G"}

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
		rows := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15", "16", "17", "18", "19", "20", "21", "22", "23", "24", "25"}
		cols := []string{"A", "B", "C", "D", "E", "F", "G"}

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
	fmt.Println("Enter commands as:")
	fmt.Println("1) clientID SeatID RequestType")
	fmt.Println("2) scale N")
	fmt.Println("Type 'done' to exit.")

	// Predefined seats for random selection
	seats := []string{
		"1A", "1B", "1C", "1D", "1E", "1F", "1G",
		"2A", "2B", "2C", "2D", "2E", "2F", "2G",
		"3A", "3B", "3C", "3D", "3E", "3F", "3G",
		"4A", "4B", "4C", "4D", "4E", "4F", "4G",
		"5A", "5B", "5C", "5D", "5E", "5F", "5G",
		"6A", "6B", "6C", "6D", "6E", "6F", "6G",
		"7A", "7B", "7C", "7D", "7E", "7F", "7G",
		"8A", "8B", "8C", "8D", "8E", "8F", "8G",
		"9A", "9B", "9C", "9D", "9E", "9F", "9G",
		"10A", "10B", "10C", "10D", "10E", "10F", "10G",
		"11A", "11B", "11C", "11D", "11E", "11F", "11G",
		"12A", "12B", "12C", "12D", "12E", "12F", "12G",
		"13A", "13B", "13C", "13D", "13E", "13F", "13G",
		"14A", "14B", "14C", "14D", "14E", "14F", "14G",
		"15A", "15B", "15C", "15D", "15E", "15F", "15G",
		"16A", "16B", "16C", "16D", "16E", "16F", "16G",
		"17A", "17B", "17C", "17D", "17E", "17F", "17G",
		"18A", "18B", "18C", "18D", "18E", "18F", "18G",
		"19A", "19B", "19C", "19D", "19E", "19F", "19G",
		"20A", "20B", "20C", "20D", "20E", "20F", "20G",
		"21A", "21B", "21C", "21D", "21E", "21F", "21G",
		"22A", "22B", "22C", "22D", "22E", "22F", "22G",
		"23A", "23B", "23C", "23D", "23E", "23F", "23G",
		"24A", "24B", "24C", "24D", "24E", "24F", "24G",
		"25A", "25B", "25C", "25D", "25E", "25F", "25G",
	}

	// Create a slice of client IDs for random selection
	clientIDsSlice := make([]string, 0, len(clients))
	for cid := range clients {
		clientIDsSlice = append(clientIDsSlice, cid)
	}

	for {
		fmt.Print("Enter your request: ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if strings.ToLower(input) == "done" {
			fmt.Println("Finishing input. Closing all request channels...")
			break
		}

		parts := strings.Fields(input)
		if len(parts) == 0 {
			continue
		}

		if strings.ToLower(parts[0]) == "scale" {
			// Format: scale N
			if len(parts) != 2 {
				fmt.Println("Invalid scale input. Format: scale N")
				continue
			}

			nStr := parts[1]
			n, err := strconv.Atoi(nStr)
			if err != nil {
				fmt.Println("N must be an integer.")
				continue
			}

			// Ensure there are enough seats to reserve
			if n > len(seats) {
				fmt.Printf("Cannot reserve %d seats; only %d available.\n", n, len(seats))
				continue
			}

			// Reserve seats incrementally
			reservedSeats := 0
			start := time.Now()
			for i := 0; i < n; i++ {
				seatID := seats[i] // Take the next available seat
				randomClientID := clientIDsSlice[rand.Intn(len(clientIDsSlice))]
				req := common.Request{
					SeatID: seatID,
					Type:   "RESERVE",
				}
				wg.Add(1) // Increment WaitGroup counter
				wgCounter++
				clients[randomClientID].HandleRequest(req)
				reservedSeats++
			}

			wg.Wait() // Wait for all requests to complete
			elapsed := time.Since(start)
			fmt.Printf("Reserved %d seats among random clients in %s\n", reservedSeats, elapsed)
			continue
		}

		if strings.ToLower(input) == "clean" {
			// Send CLEANSEATS request to a random client
			randomClientID := clientIDsSlice[rand.Intn(len(clientIDsSlice))]
			req := common.Request{
				Type: "CLEANSEATS",
			}
			wg.Add(1) // Increment WaitGroup counter
			wgCounter++
			clients[randomClientID].HandleRequest(req)
			wg.Wait() // Wait for clean operation to complete
			fmt.Println("All seats have been cleaned and reset to available.")
			continue
		}

		// Normal single request format: clientID SeatID RequestType
		if len(parts) != 3 {
			fmt.Println("Invalid input. Format: clientID SeatID RequestType or scale N or clean")
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
