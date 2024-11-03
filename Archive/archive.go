/* config.go

package main

import "time"

// LockMode represents the status of a seat.
type LockMode int

const (
	Free LockMode = iota
	Reserved
	Booked
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
)

// ChubbyNode represents a single server in the Chubby cell
type ChubbyNode struct {
	isLeader  bool
	Seats     map[string]LockMode // Mapping of seat IDs to their lock status
	locks     map[string]string   // Maps seat IDs to the client who holds the lock
	lockLimit int                 // Maximum locks allowed per client
	mutex     sync.Mutex          // Protects access to Seats and locks
}

// NewChubbyNode initializes a new ChubbyNode
func NewChubbyNode(isLeader bool) *ChubbyNode {
	node := &ChubbyNode{
		isLeader:  isLeader,
		Seats:     make(map[string]LockMode),
		locks:     make(map[string]string),
		lockLimit: 5, // Limit clients to 5 simultaneous locks
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
	seatReserved  []string //IDs of seats
	seatBooked    []string //IDs of seats
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
		go s.keepAlive()
		return nil
	}

	return fmt.Errorf("Could not connect to any server.")
}

// keepAlive sends periodic heartbeat signals to keep the session active.
func (s *Session) keepAlive() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mutex.Lock()
		if s.isExpired {
			s.mutex.Unlock()
			return
		}
		s.timeStamp = time.Now()
		s.logger.Printf("Session kept alive for client: %d", s.clientID)
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