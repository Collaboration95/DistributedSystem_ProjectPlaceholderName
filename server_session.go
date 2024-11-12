// initalize a session
// map of seats - id & lock mode
// 5 rows of seat, 5 columns
// digit 1-5 and letter A-E
// client has to access the
package main

import (
	"fmt"
	"log"
	"net/rpc"
	"sync"
	"time"
)

// seat lock modes are FREE or RESERVED

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

// timeStamp   time.Time           // track the session seconds or minutes

// this NewServerSession function should be part of a command line interaction so that we can reset all seats to FREE
// otherwise it should continue with the lock modes of the last updated sessionID

// NewServerSession initializes the ServerSession with all seats set to FREE
func NewServerSession(logger *log.Logger) *ServerSession {
	seats := make(map[string]LockMode)
	for row := 1; row <= 5; row++ {
		for col := 'A'; col <= 'E'; col++ {
			seatID := fmt.Sprintf("%d%c", row, col)
			seats[seatID] = LockMode{Type: FREE}
		}
	}
	return &ServerSession{
		locks:     seats,
		mutex:     sync.Mutex{},
		logger:    logger,
		startTime: time.Now(),
	}
}

// CheckSeatAvailability checks if a specific seat is FREE for reservation
func (s *ServerSession) CheckSeatAvailability(seatID string) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if lockMode, exists := s.locks[seatID]; exists && lockMode.Type == FREE {
		return true
	}
	return false
}

func (s *ServerSession) ReserveSeat(seatID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if lockMode, exists := s.locks[seatID]; exists && lockMode.Type == FREE {
		s.locks[seatID] = LockMode{Type: RESERVED}
		s.logger.Printf("Seat %s reserved", seatID)
		return nil
	}
	return fmt.Errorf("seat %s is not available for reservation", seatID)

}

// ReleaseSeat releases a seat by setting its lock mode back to FREE
func (s *ServerSession) ReleaseSeat(seatID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if lockMode, exists := s.locks[seatID]; exists && lockMode.Type == RESERVED {
		s.locks[seatID] = LockMode{Type: FREE}
		s.logger.Printf("Seat %s released", seatID)
		return nil
	}
	return fmt.Errorf("seat %s is not reserved", seatID)
}

// add function so that when client books a seat, the lock for that seatID gets deleted
