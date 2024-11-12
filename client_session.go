package main

// package client_session

import (
	"fmt"
	"log"
	"math/rand"
	"net/rpc"
	"os"
	"sync"
	"time"
)

// variables from server side
const (
	serverHandler   = "Server.HandlerMethod"   // handler
	serverKeepAlive = "server.KeepAliveMethod" // keep alive method
)

const (
	serverRequestLock = "Server.RequestLock"
	serverReleaseLock = "Server.ReleaseLock"
	serverDeleteLock  = "Server.DeleteLock"
)

var KnownServerAddrs = []string{
	"127.0.0.1:8002",
	"127.0.0.1:8003",
	"127.0.0.1:8004",
	"127.0.0.1:8001",
	"127.0.0.1:8000",
}

// seat has 3 status
// free | reserved | booked
// reserved means user has clicked on the seat and in the process of booking not yet finished booking

// client, representing browser, interacts with the session
type client struct {
	clientID      int
	chubbySession *Session
	seatReserved  []string //IDs of seats
	seatBooked    []string // IDs of seats
}

// chubbySession manages transactions between client and chubby server and holds locks for client

// reserve seat is done by requestlock after checking lock mode

// session manages timings and checks for activity from client
type Session struct {
	clientID    int  // who is the client initializing the session?
	sessionID   int  // added this
	isExpired   bool // has the session gone past 10 mins?
	leaseLength time.Duration
	timeStamp   time.Time           // track the session seconds or minutes
	serverAddr  string              // connection to chubby server
	rpcClient   *rpc.Client         //use rpc package to establish session server connection
	logger      *log.Logger         // Logger
	isJeopardy  bool                // flag to determine if entering grace period (45s for now)
	locks       map[string]LockMode //string represents seat ID
	mutex       sync.Mutex          //protects session operations
}

const (
	DefaultLeaseDuration time.Duration = 20 * time.Second
	JeopardyDuration     time.Duration = 45 * time.Second
)

// Define ClientID as a type for clarity in requests
type ClientID int

// InitSessionRequest is used to initiate a session with the Chubby server
type InitSessionRequest struct {
	ClientID ClientID // The ID of the client requesting the session
}

// InitSessionResponse confirms the session initialization
type InitSessionResponse struct {
	LeaseLength time.Duration // Lease length for the session
	Success     bool          // Whether initialization was successful
	Error       string        // Any error message from the server
}

// KeepAliveRequest struct used to send keep alives
type KeepAliveRequest struct {
	ClientID int
}

type KeepAliveResponse struct {
	LeaseLength time.Duration
	Success     bool
}

// defines the lock types
type LockMode struct {
	Type string // RESERVED | BOOKED | FREE
}

// lock modes as a constant
const (
	RESERVED = "RESERVED"
	BOOKED   = "BOOKED"
	FREE     = "FREE"
)

// Initialize session on client side

func (s *Session) InitSession(clientID int, KnownServerAddrs []string) error {
	// Step 1: Init session fields
	s.clientID = clientID
	// s.sessionID = ??? can be set to either a counter or random num?
	s.isExpired = false
	s.timeStamp = time.Now()
	s.isJeopardy = false
	s.locks = make(map[string]LockMode)
	s.logger = log.New(os.Stderr, "[client] ", log.LstdFlags)

	// Step 2: Attempt to connect to known Chubby servers
	for _, serverAddr := range KnownServerAddrs {
		// Try to connect to each known server address
		client, err := rpc.Dial("tcp", serverAddr)
		if err != nil {
			s.logger.Printf("Failed to connect to server at %s: %s", serverAddr, err.Error())
			continue
		}
		s.rpcClient = client
		s.serverAddr = serverAddr
		s.logger.Printf("Connected to server at %s for client: %d", serverAddr, s.clientID)

		// Step 3: Initialize session on the server
		req := InitSessionRequest{ClientID: ClientID(s.clientID)}
		resp := &InitSessionResponse{}
		err = s.rpcClient.Call(serverHandler, &req, resp)
		if err != nil {
			s.logger.Printf("InitSession RPC call failed with error: %v", err)
			return err
		}

		// If initialization is successful, set lease length and log success
		if resp.Success {
			s.leaseLength = resp.LeaseLength
			s.logger.Printf("Session initialized successfully with lease length: %v", s.leaseLength)
			s.logger.Printf("commencing session monitoring for client: %v", s.clientID)
			//Step 4: once session is initialized and able to connect to server, start monitoring session
			go s.MonitorSession()
			return nil // Exit the function upon successful initialization
		} else {
			return fmt.Errorf("session initialization failed: %s", resp.Error)
		}
	}

	// If we couldn't connect to any servers
	return fmt.Errorf("could not connect to any Chubby server")
}

func (s *Session) KeepAlive() {
	//heartbetas sent  to server side to keep seesion alive for client
	ticker := time.NewTicker(DefaultLeaseDuration)
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

type Request struct {
	SeatID   string
	ClientID string
	Type     RequestType // Type of request: RESERVE or RELEASE or BOOK
	Response chan error  // Channel to send the operation result back to the client
}

type Response struct {
	Success bool
	Error   string
}

func (s *Session) EnterJeopardy() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.isJeopardy {
		s.logger.Printf("Already in jeopardy mode for client: %d", s.clientID)
		return
	}

	s.isJeopardy = true
	s.logger.Printf("Entering jeopardy mode for client: %d", s.clientID)

	// Start a timer for the Jeopardy duration
	jeopardyTimer := time.NewTimer(JeopardyDuration)
	go func() {
		<-jeopardyTimer.C
		s.mutex.Lock()
		defer s.mutex.Unlock()

		// If the session is still in jeopardy after the grace period, mark it as expired
		if s.isJeopardy {
			s.isExpired = true
			s.isJeopardy = false
			s.logger.Printf("Session expired for client: %d after jeopardy period", s.clientID)
		}
	}()
}

func (s *Session) MonitorSession() {
	interval := s.leaseLength / 2 // Set interval to half of the lease length, or any other desired duration
	ticker := time.NewTicker(interval)
	defer ticker.Stop() //clean up ticker when monitorsession stops

	s.logger.Printf("Monitoring session with server %s at interval: %v", s.serverAddr, interval)

	// Channel to handle keep-alive responses or trigger jeopardy mode
	keepAliveChan := make(chan *KeepAliveResponse, 1)

	for {
		select {
		case <-ticker.C: // Triggered at each interval
			go func() {
				defer func() {
					if r := recover(); r != nil {
						s.logger.Printf("KeepAlive waiter encountered panic: %s; recovering", r)
					}
				}()

				req := KeepAliveRequest{ClientID: s.clientID}
				res := &KeepAliveResponse{}

				s.logger.Printf("Sending KeepAlive to server %s", s.serverAddr)
				err := s.rpcClient.Call(serverKeepAlive, req, res)
				if err != nil {
					s.logger.Printf("RPC call error: %s", err.Error())
					return // Session will time out if no response
				}

				keepAliveChan <- res
			}()

		case resp := <-keepAliveChan:
			if resp.Success {
				// Update lease length if it has changed
				if resp.LeaseLength != s.leaseLength {
					s.leaseLength = resp.LeaseLength
					interval = s.leaseLength / 2 // Update interval if lease length changed
					ticker.Reset(interval)
					s.logger.Printf("Lease length updated to %v; adjusting interval to %v", s.leaseLength, interval)
				}
				s.logger.Printf("KeepAlive successful for client %d", s.clientID)
			} else {
				s.logger.Printf("KeepAlive failed, entering jeopardy")
				s.EnterJeopardy()
			}

		case <-time.After(s.leaseLength): // Trigger jeopardy if no response before lease expiry
			s.logger.Printf("No response received, entering jeopardy")
			s.EnterJeopardy()
			return
		}
	}
}

// Request Lock of type FREE for a particular seat
func (s *Session) RequestLock(seatID string, clientID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	//checks if seatID is found in the map s.Locks
	//ok is true if found and false if not found
	lockMode, ok := s.locks[seatID]
	//if lock exists and is a RESERVED return error
	if ok && lockMode.Type == RESERVED {
		return fmt.Errorf("Seat %s is currently reserved", seatID)
	}
	req := &Request{SeatID: seatID, ClientID: clientID}
	resp := new(Response)
	// err:=s.rpcClient.Call(serverHandler.RequestLock,req,resp)
	err := s.rpcClient.Call(serverRequestLock, req, resp)

	if err != nil || !resp.Success {
		return fmt.Errorf("Unable to require lock on seat %s", seatID)
	}
	// If successful, add the RESERVED lock to the local map.
	s.locks[seatID] = LockMode{Type: RESERVED}
	s.logger.Printf("Lock aquired for seat %s by client ID %s", seatID, clientID)
	return nil
}

// Release Lock of type RESERVED for a particular seat if client changes mind, decides not to book and releases the seats
func (s *Session) ReleaseLock(seatID string, clientID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	//checks if seatID is found in the map s.Locks
	//ok is true if found and false if not found
	lockMode, ok := s.locks[seatID]
	//if no FREE lock is found for the seat return error
	if !ok || lockMode.Type != FREE {
		return fmt.Errorf("Unable to find FREE lock for seat %s ", seatID)
	}
	req := &Request{SeatID: seatID, ClientID: clientID}
	resp := new(Response)
	// err:=s.rpcClient.Call(serverHandler.ReleaseLock,req,resp)
	err := s.rpcClient.Call(serverRequestLock, req, resp)
	if err != nil || !resp.Success {
		return fmt.Errorf("Unable to release lock on seat %s", seatID)
	}
	// If successful, remove the lock from the local map.
	delete(s.locks, seatID)
	s.logger.Printf("Lock released for seat %s by client ID %s", seatID, clientID)
	return nil
}

// Delete Lock of type RESERVED for a particular seat if client BOOKS seat
func (s *Session) DeleteLock(seatID string, clientID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	req := &Request{SeatID: seatID, ClientID: clientID}
	resp := new(Response)
	// err:=s.rpcClient.Call(serverHandler.DeleteLock,req,resp)
	err := s.rpcClient.Call(serverDeleteLock, req, resp)
	if err != nil || !resp.Success {
		return fmt.Errorf("Unable to delete lock for seat %s", seatID)
	}
	// If successful, remove the RESERVED lock from the local map
	delete(s.locks, seatID)
	s.logger.Printf("Lock deleted for seat %s by client ID %s", seatID, clientID)
	return nil
}

// function to simulate client behaviour
func simulateClient(clientID int, wg *sync.WaitGroup) {
	defer wg.Done()
	session := &Session{}
	seatID := "seat1A" // Example seat ID to try and reserve for simplicity - number is for row, letter for column

	// step 1: Initialize the session for the client
	err := session.InitSession(clientID, KnownServerAddrs)
	if err != nil {
		log.Printf("Client %d failed to initialize session: %s", clientID, err.Error())
		return
	}

	// step 2: attempt to request a lock on the seat
	err = session.RequestLock(seatID, string(clientID))
	if err != nil {
		log.Printf("Client %d failed to request lock on %s: %s", clientID, seatID, err.Error())
		return
	}
	log.Printf("Client %d successfully reserved %s", clientID, seatID)

	// Step 3: Simulate some processing time, then release the lock or delete it
	time.Sleep(time.Duration(rand.Intn(5)) * time.Second)

	if rand.Float32() < 0.5 {
		// Release lock if client decides not to book
		err = session.ReleaseLock(seatID, string(clientID))
		if err != nil {
			log.Printf("Client %d failed to release lock on %s: %s", clientID, seatID, err.Error())
		} else {
			log.Printf("Client %d released lock on  %s", clientID, seatID)
		}
	} else {
		// delete lock if client books the seat
		err = session.DeleteLock(seatID, string(clientID))
		if err != nil {
			log.Printf("Client %d failed to delete lock on %s: %s", clientID, seatID, err.Error())
		} else {
			log.Printf("Client %d booked %s", clientID, seatID)
		}
	}
}

func StartClient() {
	// Simulate client initialization
	log.Println("Client started, connecting to server...")
	var wg sync.WaitGroup
	numClients := 10 // number of clients to simulate

	// start simulation for each client
	for i := 1; i <= numClients; i++ {
		wg.Add(1)
		go simulateClient(i, &wg)
	}

	// wait for all client to finish
	wg.Wait()
	log.Println("Simulation complete")
}
