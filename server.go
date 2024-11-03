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
		reply.Message = fmt.Sprintf("%s is already booked or unavailable", seatID)
		return nil
	}

	// Check if seat is reserved by another client
	if lockHolder, exists := node.locks[seatID]; exists && lockHolder != clientID {
		reply.Success = false
		reply.Message = fmt.Sprintf("%s is already reserved by another client", seatID)
		return nil
	}

	// Reserve the seat and assign lock to the client
	node.Seats[seatID] = Reserved
	node.locks[seatID] = clientID
	reply.Success = true
	reply.Message = fmt.Sprintf("%s reserved successfully", seatID)
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
		reply.Message = fmt.Sprintf("%s is not reserved by you or is unavailable", seatID)
		return nil
	}

	// Book the seat
	node.Seats[seatID] = Booked
	delete(node.locks, seatID)
	reply.Success = true
	reply.Message = fmt.Sprintf("%s booked successfully", seatID)
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
		reply.Message = fmt.Sprintf("%s is not reserved by you or is unavailable", seatID)
		return nil
	}

	// Release the lock
	node.Seats[seatID] = Free
	delete(node.locks, seatID)
	reply.Success = true
	reply.Message = fmt.Sprintf("%s lock released", seatID)
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
