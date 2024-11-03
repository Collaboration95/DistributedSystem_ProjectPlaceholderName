/* config.go

package main

import "time"

// LockMode represents the status of a seat.
type LockMode int

const (
	Free     LockMode = iota
	Reserved          // Seat is reserved but not yet booked
	Booked            // Seat is fully booked
)

// Default lease durations
const (
	DefaultLeaseDuration = 20 * time.Second
	JeopardyDuration     = 45 * time.Second
)

// Request and response structs for RPC calls
type RequestLockArgs struct {
	SeatID   string
	ClientID string
}

type RequestLockResponse struct {
	Success bool
	Message string
}

// KeepAlive request and response structs
type KeepAliveArgs struct {
	ClientID string
}

type KeepAliveResponse struct {
	Success bool
	Message string
}

*/

// // // // // // // // // // // // // // // // // // // // // // // // // // // // // // // // // // // // // // //

/* server.go

package main

import (
	"fmt"
	"log"
	"net"
	"net/rpc"
	"sync"
	"time"
)

// ChubbyNode represents a single server in the Chubby cell
type ChubbyNode struct {
	isLeader  bool
	Seats     map[string]LockMode // Mapping of seat IDs to their lock status
	locks     map[string]string   // Maps seat IDs to the client who holds the lock
	lockLimit int                 // Maximum locks allowed per client
	mutex     sync.Mutex
	clients   map[string]time.Time // Tracks last active time for clients
}

// NewChubbyNode initializes a new ChubbyNode
func NewChubbyNode(isLeader bool) *ChubbyNode {
	node := &ChubbyNode{
		isLeader:  isLeader,
		Seats:     make(map[string]LockMode),
		locks:     make(map[string]string),
		lockLimit: 5,                          // Limit clients to 5 simultaneous locks
		clients:   make(map[string]time.Time), // Initialize client tracking
	}
	// Initialize 50 seats
	for i := 1; i <= 50; i++ {
		node.Seats[fmt.Sprintf("seat-%d", i)] = Free
	}
	return node
}

// RequestLock handles lock requests from clients
func (node *ChubbyNode) RequestLock(args RequestLockArgs, reply *RequestLockResponse) error {
	node.mutex.Lock()
	defer node.mutex.Unlock()

	seatID := args.SeatID
	clientID := args.ClientID

	// Check if the seat is already locked or booked
	if currentStatus, exists := node.Seats[seatID]; !exists || currentStatus == Booked {
		reply.Success = false
		reply.Message = fmt.Sprintf("Seat %s is already booked or unavailable", seatID)
		return nil
	}

	// Check if seat is reserved by another client
	if lockHolder, exists := node.locks[seatID]; exists && lockHolder != clientID {
		reply.Success = false
		reply.Message = fmt.Sprintf("Seat %s is already reserved by another client", seatID)
		return nil
	}

	// Reserve the seat and assign lock to the client
	node.Seats[seatID] = Reserved
	node.locks[seatID] = clientID
	reply.Success = true
	reply.Message = fmt.Sprintf("Seat %s reserved successfully", seatID)
	return nil
}

// BookSeat confirms the reservation by marking a seat as booked
func (node *ChubbyNode) BookSeat(args RequestLockArgs, reply *RequestLockResponse) error {
	node.mutex.Lock()
	defer node.mutex.Unlock()

	seatID := args.SeatID
	clientID := args.ClientID

	// Ensure the seat is currently reserved by the requesting client
	if currentStatus, exists := node.Seats[seatID]; !exists || currentStatus != Reserved || node.locks[seatID] != clientID {
		reply.Success = false
		reply.Message = fmt.Sprintf("Seat %s is not reserved by you or is unavailable", seatID)
		return nil
	}

	// Book the seat
	node.Seats[seatID] = Booked
	delete(node.locks, seatID)
	reply.Success = true
	reply.Message = fmt.Sprintf("Seat %s booked successfully", seatID)
	return nil
}

// ReleaseLock releases a lock on a seat if it's in a reserved state
func (node *ChubbyNode) ReleaseLock(args RequestLockArgs, reply *RequestLockResponse) error {
	node.mutex.Lock()
	defer node.mutex.Unlock()

	seatID := args.SeatID
	clientID := args.ClientID

	// Ensure the seat is currently reserved by the requesting client
	if currentStatus, exists := node.Seats[seatID]; !exists || currentStatus != Reserved || node.locks[seatID] != clientID {
		reply.Success = false
		reply.Message = fmt.Sprintf("Seat %s is not reserved by you or is unavailable", seatID)
		return nil
	}

	// Release the lock
	node.Seats[seatID] = Free
	delete(node.locks, seatID)
	reply.Success = true
	reply.Message = fmt.Sprintf("Seat %s lock released", seatID)
	return nil
}

// KeepAlive handles keep-alive requests from clients
func (node *ChubbyNode) KeepAlive(args KeepAliveArgs, reply *KeepAliveResponse) error {
	node.mutex.Lock()
	defer node.mutex.Unlock()

	// Update the last active time for the client
	node.clients[args.ClientID] = time.Now()
	reply.Success = true
	reply.Message = "Keep-alive acknowledged"
	return nil
}

// StartServer initializes and runs the Chubby server on the specified port
func StartServer(port string, isLeader bool) error {
	node := NewChubbyNode(isLeader)

	rpc.Register(node)
	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return fmt.Errorf("failed to start server on port %s: %v", port, err)
	}
	defer listener.Close()

	log.Printf("ChubbyNode started on port %s (Leader: %v)", port, isLeader)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("connection accept error: %v", err)
			continue
		}
		go rpc.ServeConn(conn)
	}
}

func main() {
	// Start 5 nodes with one leader
	var wg sync.WaitGroup
	ports := []string{"8000", "8001", "8002", "8003", "8004"}

	for i, port := range ports {
		wg.Add(1)
		isLeader := i == 0 // First node is the leader
		go func(port string, isLeader bool) {
			defer wg.Done()
			if err := StartServer(port, isLeader); err != nil {
				log.Fatalf("Server failed on port %s: %v", port, err)
			}
		}(port, isLeader)
	}

	wg.Wait()
}

*/

// // // // // // // // // // // // // // // // // // // // // // // // // // // // // // // // // // // // // // //

/* session.go

package main

import (
	"fmt"
	"log"
	"net/rpc"
	"os"
	"sync"
	"time"
)

// Client struct representing a client in the booking system.
type Client struct {
	clientID      int
	chubbySession *Session
	seatReserved  []string // IDs of seats
	seatBooked    []string // IDs of seats
}

// Session struct manages session timings and checks for activity from clients.
type Session struct {
	clientID   int
	isExpired  bool
	timeStamp  time.Time
	serverAddr string
	rpcClient  *rpc.Client
	logger     *log.Logger
	isJeopardy bool
	locks      map[string]LockMode
	mutex      sync.Mutex
}

// Initialize a session.
func (s *Session) InitSession(clientID int, KnownServerAddrs []string) error {
	s.clientID = clientID
	s.isExpired = false
	s.timeStamp = time.Now()
	s.isJeopardy = false
	s.locks = make(map[string]LockMode)
	s.logger = log.New(os.Stderr, "[client] ", log.LstdFlags)

	for _, serverAddr := range KnownServerAddrs {
		client, err := rpc.Dial("tcp", serverAddr)
		if err != nil {
			s.logger.Printf("Failed to connect to server: %s", err.Error())
			continue
		}
		s.rpcClient = client
		s.serverAddr = serverAddr
		s.logger.Printf("Session successfully initialized for client: %d", s.clientID)
		go s.keepAlive() // Start keep-alive mechanism
		return nil
	}

	return fmt.Errorf("Could not connect to any server.")
}

func (s *Session) keepAlive() {
	ticker := time.NewTicker(1 * time.Minute) // Send keep-alive every minute
	defer ticker.Stop()
	for range ticker.C {
		s.mutex.Lock()
		if s.isExpired {
			s.mutex.Unlock()
			return
		}
		// Call the server to keep the session alive
		args := KeepAliveArgs{ClientID: fmt.Sprint(s.clientID)}
		var reply KeepAliveResponse
		err := s.rpcClient.Call("ChubbyNode.KeepAlive", args, &reply)
		if err == nil && reply.Success {
			s.timeStamp = time.Now() // Update timestamp if keep-alive acknowledged
			s.logger.Printf("Keep-alive sent for client: %d", s.clientID)
		} else {
			s.logger.Printf("Failed to send keep-alive: %v", err)
		}
		s.mutex.Unlock()
	}
}

// RequestLock tries to reserve a seat for the client.
func (s *Session) RequestLock(seatID string) error {
	args := RequestLockArgs{SeatID: seatID, ClientID: fmt.Sprint(s.clientID)}
	var reply RequestLockResponse
	err := s.rpcClient.Call("ChubbyNode.RequestLock", args, &reply)
	if err != nil {
		return err
	}
	if !reply.Success {
		return fmt.Errorf(reply.Message)
	}
	s.locks[seatID] = Reserved
	return nil
}

// BookSeat finalizes the booking of a reserved seat.
func (s *Session) BookSeat(seatID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.locks[seatID] != Reserved {
		return fmt.Errorf("Seat %s is not reserved", seatID)
	}
	args := RequestLockArgs{SeatID: seatID, ClientID: fmt.Sprint(s.clientID)}
	var reply RequestLockResponse
	err := s.rpcClient.Call("ChubbyNode.BookSeat", args, &reply)
	if err != nil {
		return err
	}
	if reply.Success {
		s.locks[seatID] = Booked
	}
	return nil
}

// ReleaseLock releases a lock on a reserved seat.
func (s *Session) ReleaseLock(seatID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.locks[seatID] != Reserved {
		return fmt.Errorf("Seat %s is not reserved", seatID)
	}
	args := RequestLockArgs{SeatID: seatID, ClientID: fmt.Sprint(s.clientID)}
	var reply RequestLockResponse
	err := s.rpcClient.Call("ChubbyNode.ReleaseLock", args, &reply)
	if err != nil {
		return err
	}
	delete(s.locks, seatID)
	return nil
}

// CloseSession closes the session and cleans up resources.
func (s *Session) CloseSession() {
	s.isExpired = true
	s.rpcClient.Close()
}

*/

// // // // // // // // // // // // // // // // // // // // // // // // // // // // // // // // // // // // // // //

/* main.go

package main

import (
	"fmt"
	"log"
	"time"
)

// Helper function to check errors
func checkError(err error) {
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func main() {
	// Simulate client interactions
	leaderAddress := "localhost:8000" // Leader node address
	clientID := 1                     // Simulated client ID
	clientID2 := 2                    // Simulated second client ID

	// Initialize a session for the first client
	session := &Session{}
	err := session.InitSession(clientID, []string{leaderAddress})
	checkError(err)

	// Test locking a seat
	fmt.Println("Attempting to reserve seat-1 by Client 1...")
	err = session.RequestLock("seat-1")
	checkError(err)
	fmt.Println("Seat-1 reserved successfully by Client 1.")

	// Initialize a session for the second client
	session2 := &Session{}
	err = session2.InitSession(clientID2, []string{leaderAddress})
	checkError(err)

	// Test that Client 2 cannot request a lock on a reserved seat (seat-1)
	fmt.Println("Attempting to reserve seat-1 by Client 2...")
	err = session2.RequestLock("seat-1")
	if err != nil {
		fmt.Printf("Client 2 failed to reserve seat-1: %v\n", err)
	} else {
		fmt.Println("Client 2 reserved seat-1 successfully (unexpected).")
	}

	// Test booking the reserved seat
	fmt.Println("Client 1 attempting to book seat-1...")
	err = session.BookSeat("seat-1")
	checkError(err)
	fmt.Println("Seat-1 booked successfully by Client 1.")

	// Test that Client 2 cannot request a lock for a booked seat (seat-1)
	fmt.Println("Attempting to reserve seat-1 by Client 2 again...")
	err = session2.RequestLock("seat-1")
	if err != nil {
		fmt.Printf("Client 2 failed to reserve seat-1 (expected): %v\n", err)
	} else {
		fmt.Println("Client 2 reserved seat-1 successfully (unexpected).")
	}

	// Test locking another seat by Client 2
	fmt.Println("Client 2 attempting to reserve seat-2...")
	err = session2.RequestLock("seat-2")
	checkError(err)
	fmt.Println("Seat-2 reserved successfully by Client 2.")

	// Test releasing a lock on seat-2
	fmt.Println("Client 2 releasing seat-2 lock...")
	err = session2.ReleaseLock("seat-2")
	checkError(err)
	fmt.Println("Seat-2 lock released by Client 2.")

	// Test keep-alive functionality
	fmt.Println("Starting keep-alive for Client 1...")
	go func() {
		for {
			// Keep-alive every 30 seconds
			time.Sleep(30 * time.Second)
			// Call the keep-alive function
			args := KeepAliveArgs{ClientID: fmt.Sprint(clientID)}
			var reply KeepAliveResponse
			err := session.rpcClient.Call("ChubbyNode.KeepAlive", args, &reply)
			if err == nil && reply.Success {
				fmt.Println("Keep-alive sent for Client 1.")
			} else {
				fmt.Printf("Failed to send keep-alive for Client 1: %v\n", err)
			}
		}
	}()

	// Allow some time for keep-alive messages to be sent
	time.Sleep(90 * time.Second)

	// Close the sessions after use
	session.CloseSession()
	session2.CloseSession()
	fmt.Println("Sessions closed.")
}

*/