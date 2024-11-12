// initalize a session
// map of seats - id & lock mode
// 5 rows of seat, 5 columns
// digit 1-5 and letter A-E
// client has to access the
package main

import (
	"fmt"
	"log"
	"net"
	"net/rpc"
	"os"
	"sync"
	"time"
)

// seat lock modes are FREE or RESERVED
// LockMode defines the status of a seat

// RequestType specifies if the operation is to reserve or release a seat
type RequestType string

type RequestArgs struct {
	ClientID string
	SeatID   string
}

type ServerResponse struct {
	Err error
}

const (
	Reserve RequestType = "RESERVE"
	Release RequestType = "RELEASE"
	Book    RequestType = "BOOK"
)

// Shared request channel for handling all incoming requests sequentially across all sessions
var requestChan = make(chan Request, 100)

// Global mutex to protect seat locks across all sessions of server
var globalMutex = sync.Mutex{}

// // Centralized map of seats and their lock modes
// var locks = make(map[string]LockMode)

// SeatLock holds the status of the seat along with the client who has locked or reserved it
type SeatLock struct {
	Type     string // FREE or RESERVED
	ClientID string // ID of the client who has locked or reserved the seat
}

// Global map to store locks and associated client information
var locks = make(map[string]SeatLock)

// TODO 12 NOV 756 PM ADD A WAY TO ASSOCIATE CLIENT AND LOCK MODE

// TODO :this function should be called using command line at start of demo and when we want to reset all the locks to FREE
// TODO :otherwise initialization of server session should use a last updated session's lock map
func InitSeats() {
	for row := 1; row <= 5; row++ {
		for col := 'A'; col <= 'E'; col++ {
			seatID := fmt.Sprintf("%d%c", row, col)
			// locks[seatID] = LockMode{Type: FREE}
			locks[seatID] = SeatLock{Type: FREE}
		}
	}
}

// Session struct to manage seat locks and sessions for clients on the server side
type ServerSession struct {
	clientID    int                 // who is the client initializing the session?
	serverAddr  string              // identity of chubby server
	sessionID   int                 // added this
	isExpired   bool                // has the session gone past 10 mins?
	locks       map[string]LockMode // Map of seat IDs (e.g., "1A", "2B") to their lock modes - string represents seatID
	mutex       sync.Mutex          // Mutex to control concurrent access to seat locks
	logger      *log.Logger         // Logger for session activities
	startTime   time.Time           // To track session start time
	isJeopardy  bool                // flag to determine if entering grace period (45s for now)
	leaseLength time.Duration
	rpcClient   *rpc.Client //use rpc package to establish session server connection
}

// this NewServerSession function should be part of a command line interaction so that we can reset all seats to FREE
// otherwise it should continue with the lock modes of the last updated sessionID

// NewServerSession initializes a new ServerSession instance
func NewServerSession(clientID int, logger *log.Logger) *ServerSession {
	return &ServerSession{
		clientID:  clientID,
		logger:    logger,
		startTime: time.Now(),
	}
}

// MonitorLockRequestRelease handles all requests in the order they arrive
func MonitorLockRequestRelease() {
	for req := range requestChan {
		var err error
		globalMutex.Lock()
		switch req.Type {
		case Reserve:
			err = processReserve(req.SeatID, req.ClientID)
		case Release:
			err = processRelease(req.SeatID, req.ClientID)
		case Book:
			err = processBook(req.SeatID, req.ClientID)
		}
		globalMutex.Unlock()
		req.Response <- err // Send the result back to the client
		close(req.Response) // Close the response channel after sending the result

	}
}

// processReserve reserves a seat if it is available
func processReserve(seatID string, clientID string) error {
	if lockMode, exists := locks[seatID]; exists && lockMode.Type == FREE {
		// locks[seatID] = LockMode{Type: RESERVED}
		locks[seatID] = SeatLock{Type: RESERVED, ClientID: clientID}
		// TODO : to check if in the current session, the lock mode is RESERVED with same client ID that has sent the request to Book
		log.Printf("Seat %s reserved by client %s", seatID, clientID)
		return nil
	}
	return fmt.Errorf("seat %s is not available for reservation", seatID)
}

// processRelease releases a seat if it is currently reserved by the same client
func processRelease(seatID string, clientID string) error {
	if lockMode, exists := locks[seatID]; exists && lockMode.Type == RESERVED && lockMode.ClientID == clientID {
		// locks[seatID] = LockMode{Type: FREE}
		locks[seatID] = SeatLock{Type: FREE}
		log.Printf("Seat %s released by client %s", seatID, clientID)
		return nil
	}
	return fmt.Errorf("seat %s is not reserved by client %s", seatID, clientID)
}

// processBook books a seat if it is currently reserved by the requesting client
func processBook(seatID string, clientID string) error {
	if lockMode, exists := locks[seatID]; exists && lockMode.Type == RESERVED && lockMode.ClientID == clientID {
		delete(locks, seatID) // Remove seat from the locks map to complete the booking
		log.Printf("Seat %s booked by client %s", seatID, clientID)
		return nil
	}
	return fmt.Errorf("seat %s is not reserved by client %s or already booked", seatID, clientID)
}

// RequestLock sends a lock request to the server to reserve a seat
// func (s *ServerSession) RequestLock(seatID string) error {
// 	responseChan := make(chan error)
// 	requestChan <- Request{
// 		ClientID: string(s.clientID),
// 		SeatID:   seatID,
// 		Type:     Reserve,
// 		Response: responseChan,
// 	}
// 	return <-responseChan // Wait for the result from MonitorLockRequestRelease
// }

// RequestLock sends a lock request to the server to reserve a seat
// Updated methods to be compatible with RPC requirements
func (s *ServerSession) RequestLock(args *RequestArgs, reply *ServerResponse) error {
	responseChan := make(chan error)
	requestChan <- Request{
		ClientID: args.ClientID,
		SeatID:   args.SeatID,
		Type:     Reserve,
		Response: responseChan,
	}
	reply.Err = <-responseChan // Store the error in reply struct
	return reply.Err
}

// ReleaseLock sends a release request to the server to release a reserved seat
// func (s *ServerSession) ReleaseLock(seatID string) error {
// 	responseChan := make(chan error)
// 	requestChan <- Request{
// 		ClientID: string(s.clientID),
// 		SeatID:   seatID,
// 		Type:     Release,
// 		Response: responseChan,
// 	}
// 	return <-responseChan // Wait for the result from MonitorLockRequestRelease
// }

func (s *ServerSession) ReleaseLock(args *RequestArgs, reply *ServerResponse) error {
	responseChan := make(chan error)
	requestChan <- Request{
		ClientID: args.ClientID,
		SeatID:   args.SeatID,
		Type:     Release,
		Response: responseChan,
	}
	reply.Err = <-responseChan // Store the error in reply struct
	return reply.Err
}

// BookSeat sends a booking request to the server to book a reserved seat
// func (s *ServerSession) BookSeat(seatID string) error {
// 	responseChan := make(chan error)
// 	requestChan <- Request{
// 		ClientID: string(s.clientID),
// 		SeatID:   seatID,
// 		Type:     Book,
// 		Response: responseChan,
// 	}
// 	return <-responseChan // Wait for the result from MonitorLockRequestRelease
// }

func (s *ServerSession) BookSeat(args *RequestArgs, reply *ServerResponse) error {
	responseChan := make(chan error)
	requestChan <- Request{
		ClientID: args.ClientID,
		SeatID:   args.SeatID,
		Type:     Book,
		Response: responseChan,
	}
	reply.Err = <-responseChan // Store the error in reply struct
	return reply.Err
}

// could be main function
func Server_Session() {
	// logger := log.Default()
	// Create a logger for the server
	logger := log.New(os.Stdout, "[server] ", log.LstdFlags)

	// Log server startup message
	logger.Println("Server started")

	// Initialize seats globally and start monitoring requests
	logger.Println("Initializing seats and starting request monitor...")
	InitSeats()
	go MonitorLockRequestRelease()

	// Register ServerSession methods with RPC so they can be called by the client
	server := new(ServerSession)
	err := rpc.Register(server)
	if err != nil {
		logger.Fatal("Error registering ServerSession:", err)
	}

	// Start listening on port 8000
	listener, err := net.Listen("tcp", "127.0.0.1:8000")
	if err != nil {
		logger.Fatal("Error starting server:", err)
	}
	defer listener.Close()
	logger.Println("Server is listening on port 8000...")

	// Accept incoming connections in a loop
	for {
		conn, err := listener.Accept()
		if err != nil {
			logger.Println("Connection error:", err)
			continue
		}
		logger.Println("Accepted new connection...")
		go func(c net.Conn) {
			logger.Println("Handling new RPC connection...")
			rpc.ServeConn(c) // Handle the connection in a new goroutine
		}(conn)
	}
}

// // could be main function
// func server_session() {
// 	logger := log.Default()
// 	InitSeats()                    // Initialize seats globally
// 	go MonitorLockRequestRelease() // Start the centralized request monitor

// 	// Example usage of ServerSession
// 	session := NewServerSession(1, logger)
// 	err := session.RequestLock("1A")
// 	if err != nil {
// 		logger.Println("Failed to reserve:", err)
// 	} else {
// 		logger.Println("Seat reserved successfully.")
// 	}

// 	err = session.BookSeat("1A")
// 	if err != nil {
// 		logger.Println("Failed to book:", err)
// 	} else {
// 		logger.Println("Seat booked successfully.")
// 	}
// }

// // CheckSeatAvailability checks if a specific seat is FREE for reservation
// func (s *ServerSession) CheckSeatAvailability(seatID string) bool {
// 	s.mutex.Lock()
// 	defer s.mutex.Unlock()
// 	if lockMode, exists := s.locks[seatID]; exists && lockMode.Type == FREE {
// 		return true
// 	}
// 	return false
// }

// func (s *ServerSession) ReserveSeat(seatID string) error {
// 	s.mutex.Lock()
// 	defer s.mutex.Unlock()
// 	if lockMode, exists := s.locks[seatID]; exists && lockMode.Type == FREE {
// 		s.locks[seatID] = LockMode{Type: RESERVED}
// 		s.logger.Printf("Seat %s reserved", seatID)
// 		return nil
// 	}
// 	return fmt.Errorf("seat %s is not available for reservation", seatID)

// }

// // ReleaseSeat releases a seat by setting its lock mode back to FREE
// func (s *ServerSession) ReleaseSeat(seatID string) error {
// 	s.mutex.Lock()
// 	defer s.mutex.Unlock()
// 	if lockMode, exists := s.locks[seatID]; exists && lockMode.Type == RESERVED {
// 		s.locks[seatID] = LockMode{Type: FREE}
// 		s.logger.Printf("Seat %s released", seatID)
// 		return nil
// 	}
// 	return fmt.Errorf("seat %s is not reserved", seatID)
// }

// add function so that when client books a seat, the lock for that seatID gets deleted
