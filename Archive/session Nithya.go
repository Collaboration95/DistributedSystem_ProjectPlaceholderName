// package main

// import (
// 	"fmt"
// 	"time"
// 	"sync"
// 	"log"
// 	"net/rpc"
// 	"os"
// )

// /*
// 1.Session Initialization
// 2.Keeping Alive
// -->Client saying I am still there
// -->If client does not respond, client is no longer there
// -->Clear cache
// 3.Locking Functions
// -->Open lock to get access to writing privilege for seat ( First request to server just in case someone already has lock for the seat)
// -->Maintain lock when choosing multiple seats
// -->Release lock after use
// -->Time out to delete lock if client goes beyond time stamp
// 4.Server
// -->Client Realises that it did not receive message from client
// -->Client to Establish new connection with new master when old master fails

// Struct that may needed (client with id, session is the library )
// session.go is possibly the library
// */

// //seat has 3 status
// //free - not reserved or booked
// //reserved - in the process of booking but not booked
// //booked - booked

// //Client, representing browser, interacts with session
// type Client struct{
// 	clientID int
// 	chubbySession *Session
// 	seatReserved []string //IDs of seats
// 	seatBooked []string //IDs of seats
// }

// //chubby session manages transactions between client and chubby and hold locks for client

// //client-session methods

// func (client *Client) ReserveSeat(seatID string, clientId string) {

// 	// ask chubby session to request write lock
// 	// if denied write lock access, chubby session send failure message ("seat %s is currently reserved, booked, or unavailable", seatID)
// 	// client will do hot reload on that particular seat to show reserved

// 	// if successful, chubby session will store write lock, and send success acknowledgement back to client
// 	// client then updates seat status to reserved in DB and update UI in seat status on webpage
// }

// function (client *Client) BookSeat(seatID string, clientID string) {

// 	//go through all reserved seats
// 	//update DB of reserved seats to booked
// 	//send lock release

// }

// //--------------------------------------------------------------------------------------------

// // Session, manages session timings and checks for activity from client
// type Session struct{
// 	clientID int // who is the client initializing the session?
// 	isExpired bool // has the session gone past 10 mins?
// 	timeStamp time.Time // track the session seconds or minutes
// 	serverAddr string //connection to chubby server
// 	rpcClient *rpc.Client //use rpc package to establish session server connection
// 	logger *log.Logger	// Logger
// 	isJeopardy bool // flag to determine if entering grace period (45s for now)
// 	//LockMode 1.)Read (non exclusive), 2.)Write (exclusive)
// 	locks map[string]LockMode //string represents seat ID
// 	mutex sync.Mutex //protects session operations
// }

// const DefaultLeaseDuration time.Duration = 20 * time.Second
// const JeopardyDuration time.Duration = 45 * time.Second

// //Initialize session
// func (s *Session) InitSession(clientID int, KnownServerAddrs []string) error{
// 	s.clientID = clientID
// 	s.isExpired = false
// 	s.timeStamp = time.Now()
// 	s.isJeopardy = false
// 	s.locks = make(map[string]LockMode)
// 	s.logger = log.New(os.Stderr, "[client] ", log.LstdFlags)

// 	for _,serverAddr := range KnownServerAddrs {
// 		// search all known chubby servers and establish connection with leader server
// 		client, err := rpc.Dial("tcp", serverAddr)
// 		if err!=nil{
// 			s.logger.Printf("Failed to connect to server: %s", err.Error())
// 			continue
// 		}
// 		s.rpcClient=client
// 		s.serverAddr=serverAddr
// 		s.logger.Printf("Session successfully initialized for client: %d", s.clientID)
// 		go s.keepAlive()
// 		return nil
// 		}

// 	if s.serverAddr == "" {
// 		return nil, errors.New("Could not connect to any server.")
// 	}
// 	return fmt.Errorf("Could not connect to any server.")
// }

// func (s *Session) KeepAlive(){
// 	//heartbetas sent  to server side to keep seesion alive for client
// 	ticker:=time.NewTicker(1* time.Minute)
// 	defer ticker.Stop()
// 	for range ticker.c{
// 		s.mutex.Lock()
// 		if s.isExpired{
// 			s.mutex.Unlock()
// 			return
// 		}
// 		s.timeStamp=time.Now()
// 		s.logger.Printf("Session kept alive for client: %d", s.clientID)
// 		s.mutex.Unlock()
// 	}
// }

// func (s *Session) MonitorSession(){
// 	s.logger.Printf("Monitoring session with server %s", sess.serverAddr)
// 	//send keepalives from session to server
// 	//monitor keepalives from server to session
// 	keepAliveChan := make(chan someStructToBeConfirmed, 1)
// 		// Send a KeepAlive, waiting for a response from the master.
// 		go func() {
// 			defer func() {
// 				if r := recover(); r != nil {
// 					s.logger.Printf("KeepAlive waiter encounted panic: %s; recovering", r)
// 				}
// 			}()

// 			req := s.KeepAlive(ClientID: sess.clientID)
// 			resp :=

// 			s.logger.Printf("Sending KeepAlive to server %s", s.serverAddr)
// 			s.logger.Printf("Client ID is %s", string(s.clientID))
// 			err := s.rpcClient.Call("Handler.KeepAlive", req, resp)
// 			if err != nil {
// 				s.logger.Printf("rpc call error: %s", err.Error())
// 				return // do not push anything onto channel -- session will time out
// 			}

// 			keepAliveChan <- resp
// 		}()
// 	//Detect and handle Jeopardy
// 	//
// }

// //Request Lock of type write for a particular seat
// func (s *Session) RequestLock(seatID string, clientID string) error{

// }

// //Release Lock of type write for a particular seat if client changes mind
// func (s *Session) ReleaseLock(seatID string, clientID string) error{

// }

// //Delete Lock of type write for a particular seat if client chooses seat
// func (s *Session) DeleteLock(seatID string, clientID string) error{

// }


