package main

import (
	"fmt"
	"time"
	"sync"
	"log"
	"net/rpc"
	"os"
)
//variables required from server side
const (
    serverHandler    = "Server.HandlerMethod" // handler
    serverKeepAlive  = "Server.KeepAliveMethod" // keep-alive method
)

//seat has 3 status
//free - not reserved or booked
//reserved - in the process of booking but not booked
//booked - booked

//Client, representing browser, interacts with session
type Client struct{
	clientID int
	chubbySession *Session
	seatReserved []string //IDs of seats
	seatBooked []string //IDs of seats
}

//chubby session manages transactions between client and chubby and hold locks for client

//client-session methods

func (client *Client) ReserveSeat(seatID string, clientId string) {

	
	// ask chubby session to request write lock
	// if denied write lock access, chubby session send failure message ("seat %s is currently reserved, booked, or unavailable", seatID)
	// client will do hot reload on that particular seat to show reserved

	// if successful, chubby session will store write lock, and send success acknowledgement back to client
	// client then updates seat status to reserved in DB and update UI in seat status on webpage
}

func (client *Client) BookSeat(seatID string, clientID string) {

	//go through all reserved seats
	//update DB of reserved seats to booked
	//send lock release

}

//--------------------------------------------------------------------------------------------

// Session, manages session timings and checks for activity from client
type Session struct{
	clientID int // who is the client initializing the session?
	isExpired bool // has the session gone past 10 mins? 
	timeStamp time.Time // track the session seconds or minutes
	serverAddr string //connection to chubby server
	rpcClient *rpc.Client //use rpc package to establish session server connection
	logger *log.Logger	// Logger
	isJeopardy bool // flag to determine if entering grace period (45s for now)
	//LockMode 1.)Read (non exclusive), 2.)Write (exclusive)
	locks map[string]LockMode //string represents seat ID
	mutex sync.Mutex //protects session operations
}

const(
	DefaultLeaseDuration time.Duration = 20 * time.Second
	JeopardyDuration time.Duration = 45 * time.Second	
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

//defines the lock types
type LockMode struct{
	Type string // either write or read?
}

//lock types as a constant
const (
	ReadLock="read"
	WriteLock="write"
)

//Initialize session
func (s *Session) InitSession(clientID int, KnownServerAddrs []string) error{
	// Step 1: Init session fields
	s.clientID = clientID
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
		req := InitSessionRequest{ClientID: s.clientID}
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

func (s *Session) KeepAlive(){
	//heartbetas sent  to server side to keep seesion alive for client
	ticker:=time.NewTicker(DefaultLeaseDuration)
	defer ticker.Stop()
	for range ticker.c{
		s.mutex.Lock()
		if s.isExpired{
			s.mutex.Unlock()
			return
		}
		s.timeStamp=time.Now()
		s.logger.Printf("Session kept alive for client: %d", s.clientID)
		s.mutex.Unlock()
	}
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

//Request Lock of type write for a particular seat
func (s *Session) RequestLock(seatID string, clientID string) error{
	s.mutex.Lock()
	defer s.mutex.Unlock()
	//checks if seatID is found in the map s.Locks
	//ok is true if found and false if not found
	lockMode,ok:=s.locks[seatID]
	//if lock exists and is a writelock return error
	if ok && lockMode.Type==WriteLock{
		return fmt.Errorf("Seat %s is currently locked", seatID)
	}
	req:=&Request{SeatID:seatID, ClientID:clientID}
	resp:=new(Response)
	err:=s.rpcClient.Call(serverHandler.RequestLock,req,resp)
	if err!=nil||!resp.Success{
		return fmt.Errorf("Unable to require lock on seat %s", seatID)
	}
	// If successful, add the write lock to the local map.
	s.locks[seatID]=LockMode{Type:WriteLock}
	s.logger.Printf("Lock aquired for seat %s by client ID %s",seatID,clientID)
	return nil
}

//Release Lock of type write for a particular seat if client changes mind
func (s *Session) ReleaseLock(seatID string, clientID string) error{
	s.mutex.Lock()
	defer s.mutex.Unlock()
	//checks if seatID is found in the map s.Locks
	//ok is true if found and false if not found
	lockMode,ok:=s.locks[seatID]
	//if no write lock is found for the seat return error
	if !ok || lockMode.Type!=WriteLock{
		return fmt.Errorf("Unable to find write lock for seat %s ", seatID)
	}
	req:=&Request{SeatID:seatID, ClientID:clientID}
	resp:=new(Response)
	err:=s.rpcClient.Call(serverHandler.ReleaseLock,req,resp)
	if err!=nil||!resp.Success{
		return fmt.Errorf("Unable to release lock on seat %s", seatID)
	}
	// If successful, remove the write lock from the local map.
	delete(s.locks,seatID)
	s.logger.Printf("Lock released for seat %s by client ID %s",seatID,clientID)
	return nil
}

//Delete Lock of type write for a particular seat if client chooses seat
func (s *Session) DeleteLock(seatID string, clientID string) error{
	s.mutex.Lock()
	defer s.mutex.Unlock()
	req:=&Request{SeatID:seatID, ClientID:clientID}
	resp:=new(Response)
	err:=s.rpcClient.Call(serverHandler.DeleteLock,req,resp)
	if err!=nil||!resp.Success{
		return fmt.Errorf("Unable to delete lock for seat %s", seatID)
	}
	// If successful, remove the write lock from the local map.
	delete(s.locks,seatID)
	s.logger.Printf("Lock deleted for seat %s by client ID %s",seatID,clientID)
	return nil
}

