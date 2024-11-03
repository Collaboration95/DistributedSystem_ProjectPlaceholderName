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
		return fmt.Errorf("%s is not reserved", seatID)
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
		if s.locks[seatID] == Booked {
			return fmt.Errorf("%s is already booked and there is no refund lol", seatID)
		}
		return fmt.Errorf("%s is not reserved", seatID)
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
