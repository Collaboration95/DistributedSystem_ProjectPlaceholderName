package client

// DistributedSystemsProject\DistributedSystem_ProjectPlaceholderName\api\api.go
import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/rpc"
	"os"
	"sync"
	"time"

	"github.com/Collaboration95/DistributedSystem_ProjectPlaceholderName.git/api"
)

type ClientSession struct {
	// Client ID
	clientID api.ClientID

	// Server address
	serverAddr string

	// RPC client
	rpcClient *rpc.Client

	// Record start time
	startTime time.Time

	// Local lease length
	leaseLength time.Duration

	// Locks held by the session
	locks map[api.FilePath]api.LockMode

	// Are we in jeopardy right now?
	jeopardyFlag bool

	// Channel for notifying if jeopardy has ended
	jeopardyChan chan struct{}

	// Did this session expire?
	expired bool

	// Logger
	logger *log.Logger

	mutex sync.Mutex //protects session operations
}

//seat has 3 status
//free - not reserved or booked
//reserved - in the process of booking but not booked
//booked - booked

// Client, representing browser, interacts with session
type Client struct {
	clientID      int
	chubbySession *ClientSession //seat has 3 status

	seatReserved []string //IDs of seats
	seatBooked   []string //IDs of seats
}

const DefaultLeaseDuration time.Duration = 12 * time.Second
const JeopardyDuration time.Duration = 45 * time.Second

// Set up a Chubby session and periodically send KeepAlives to the server.
// This method should be run as a new goroutine by the client.
func InitSession(clientID api.ClientID) (*ClientSession, error) {
	// step 1: Initialize a session. Init session fields
	sess := &ClientSession{
		clientID:     clientID,
		startTime:    time.Now(),
		leaseLength:  DefaultLeaseDuration,
		locks:        make(map[api.FilePath]api.LockMode),
		jeopardyFlag: false,
		jeopardyChan: make(chan struct{}, 2),
		expired:      false,
		logger:       log.New(os.Stderr, "[client] ", log.LstdFlags),
	}

	// Find leader by trying to establish a session with any of the
	// possible server addresses.
	// Step 2: Attempt to connect to known chubby servers
	for serverAddr := range PossibleServerAddrs {
		// Try to set up TCP connection to server. - Try to connect to each known server address
		rpcClient, err := rpc.Dial("tcp", serverAddr)
		if err != nil {
			sess.logger.Printf("Failed to connect to server at %s . RPC Dial error: %s", serverAddr, err.Error())
			continue
		}

		//Step 3: Make RPC call. Initialize a session on the server
		sess.logger.Printf("Sending InitSession request to server %s", serverAddr)
		req := api.InitSessionRequest{ClientID: clientID}
		resp := &api.InitSessionResponse{}
		err = rpcClient.Call("Handler.InitSession", req, resp)
		if err == io.ErrUnexpectedEOF {
			for {
				err = rpcClient.Call("Handler.InitSession", req, resp)
				if err != io.ErrUnexpectedEOF {
					break
				}
			}
		}
		if err != nil {
			sess.logger.Printf("InitSession with server %s failed with error %s", serverAddr, err.Error())
		} else {
			sess.logger.Printf("Session with %s initialized at client", serverAddr)

			// Update session info.
			sess.serverAddr = serverAddr
			sess.rpcClient = rpcClient
			break
		}
	}

	if sess.serverAddr == "" {
		return nil, errors.New("could not connect to any server")
	}

	// Call MonitorSession.
	go sess.MonitorSession()

	return sess, nil
}

func (sess *ClientSession) MonitorSession() {
	sess.logger.Printf("Monitoring session with server %s", sess.serverAddr)
	for {
		// Make new keepAlive channel.
		// This should be ok because this loop only occurs every 12 seconds to 57 seconds.
		keepAliveChan := make(chan *api.KeepAliveResponse, 1)
		// Make a new channel to stop goroutines
		quitChan := make(chan struct{})

		// Send a KeepAlive, waiting for a response from the master.
		go func() {
			defer func() {
				if r := recover(); r != nil {
					sess.logger.Printf("KeepAlive waiter encounted panic: %s; recovering", r)
				}
			}()

			req := api.KeepAliveRequest{ClientID: sess.clientID}
			resp := &api.KeepAliveResponse{}

			sess.logger.Printf("Sending KeepAlive to server %s", sess.serverAddr)
			sess.logger.Printf("Client ID is %s", string(sess.clientID))
			err := sess.rpcClient.Call("Handler.KeepAlive", req, resp)
			if err != nil {
				sess.logger.Printf("rpc call error: %s", err.Error())
				return // do not push anything onto channel -- session will time out
			}

			keepAliveChan <- resp
		}()

		// Set up timeout
		durationLeaseOver := time.Until(sess.startTime.Add(sess.leaseLength))
		durationJeopardyOver := time.Until(sess.startTime.Add(sess.leaseLength + JeopardyDuration))

		select {
		// TODO PURPOSE OF TICKER CASE IN NITHYA CHEE KIAT BRANCH?
		case resp := <-keepAliveChan:
			// Process master's response
			// The master's response should contain a new, extended lease timeout.
			sess.logger.Printf("KeepAlive response from %s received within lease timeout", sess.serverAddr)

			// Adjust new lease length.
			if sess.leaseLength >= resp.LeaseLength {
				sess.logger.Printf("WARNING: new lease length shorter than current lease length")
			}
			sess.leaseLength = resp.LeaseLength

		case <-time.After(durationLeaseOver): // Trigger jeopardy if no response before lease expiry
			// Jeopardy period begins
			// If no response within local lease timeout, we have to block all RPCs
			// from the client until the jeopardy period is over.
			sess.jeopardyFlag = true
			sess.logger.Printf("session with %s in jeopardy", sess.serverAddr)

			// In a new goroutine, try to send KeepAlives to every server.
			// KeepAlive should check if the node is the master -> if not, ignore.
			// In KeepAlive request, eagerly send session information to server (leaseLength, locks)
			// Update session serverAddr.
			go func() {
				defer func() {
					if r := recover(); r != nil {
						sess.logger.Printf("KeepAlive waiter tried to send on closed channel: recovering")
					}
				}()

				// Jeopardy KeepAlives should allow client to eagerly send info
				// to help new leader rebuild in-mem structs
				req := api.KeepAliveRequest{
					ClientID: sess.clientID,
					Locks:    make(map[api.FilePath]api.LockMode),
				}

				for filePath, lockMode := range sess.locks {
					sess.logger.Printf("Add lock %s to KeepAlive session info", filePath)
					req.Locks[filePath] = lockMode
				}

				resp := &api.KeepAliveResponse{}

				for { // Keep trying all servers: this way we can wait for cell to elect a new leader.
					select {
					case <-quitChan:
						return
					default:
					}
					for serverAddr := range PossibleServerAddrs {
						// Try to connect to server
						rpcClient, err := rpc.Dial("tcp", serverAddr)
						if err != nil {
							sess.logger.Printf("could not dial address %s", serverAddr)
							continue
						}

						// Try to send KeepAlive to server
						sess.logger.Printf("sending KeepAlive to server %s", serverAddr)
						err = rpcClient.Call("Handler.KeepAlive", req, resp)
						if err == nil {
							// Successfully contacted new leader!
							sess.logger.Printf("received KeepAlive resp from server %s", serverAddr)

							// Update session details
							sess.serverAddr = serverAddr
							sess.rpcClient = rpcClient
							sess.startTime = time.Now()
							sess.leaseLength = DefaultLeaseDuration

							// Send response onto channel
							sess.logger.Printf("Sending response onto keepAliveChan")
							keepAliveChan <- resp
							sess.logger.Printf("Sent response onto keepAliveChan")

							return // Avoid closing new rpc client
						} else {
							sess.logger.Printf("KeepAlive error from server at %s: %s", serverAddr, err.Error())
						}

						rpcClient.Close()
					}
				}
			}()

			sess.logger.Printf("waiting for jeopardy responses")

			// Wait for responses.
			select {
			case resp := <-keepAliveChan:
				// Session is saved!
				sess.logger.Printf("session with %s safe", sess.serverAddr)

				// Process master's response
				if sess.leaseLength == resp.LeaseLength {
					// Tear down the session.
					sess.expired = true
					close(keepAliveChan)
					close(quitChan) // Stop waiting goroutines.
					err := sess.rpcClient.Close()
					if err != nil {
						sess.logger.Printf("rpc close error: %s", err.Error())
					}
					sess.logger.Printf("session with %s torn down", sess.serverAddr)
					return
				}

				// Adjust new lease length.
				if sess.leaseLength > resp.LeaseLength {
					sess.logger.Printf("WARNING: new lease length shorter than current lease length")
				}
				sess.leaseLength = resp.LeaseLength

				// Unblock all requests.
				sess.jeopardyFlag = false
				sess.jeopardyChan <- struct{}{}

			case <-time.After(durationJeopardyOver):
				// Jeopardy period ends -- tear down the session
				sess.expired = true
				close(keepAliveChan)
				close(quitChan) // Stop waiting goroutines.
				err := sess.rpcClient.Close()
				if err != nil {
					sess.logger.Printf("rpc close error: %s", err.Error())
				}
				sess.logger.Printf("session with %s expired", sess.serverAddr)
				return
			}
		}
	}
}

// OPERATIONS ON THE LOCK BY CLIENT SIDE
// OPERATION 1 ON LOCK:

// Current plan is to implement a function for each Chubby library call.
// Each function should check jeopardyFlag to see if call should be blocked.
func (sess *ClientSession) OpenLock(filePath api.FilePath) error {
	if sess.jeopardyFlag {
		durationJeopardyOver := time.Until(sess.startTime.Add(sess.leaseLength + JeopardyDuration))
		select {
		case <-sess.jeopardyChan:
			sess.logger.Printf("session with %s reestablished", sess.serverAddr)
		case <-time.After(durationJeopardyOver):
			return errors.New(fmt.Sprintf("session with %s expired", sess.serverAddr))
		}
	}
	sess.logger.Printf("Sending OpenLock request to server %s", sess.serverAddr)
	req := api.OpenLockRequest{ClientID: sess.clientID, FilePath: filePath}
	resp := &api.OpenLockResponse{}

	var err error
	for { // If we get a connection problem, keep trying.
		err = sess.rpcClient.Call("Handler.OpenLock", req, resp)
		if err != io.ErrUnexpectedEOF {
			break
		}
	}

	if err != nil {
		sess.logger.Printf("OpenLock with server %s failed with error %s", sess.serverAddr, err.Error())
	} else {
		sess.logger.Printf("Open Lock successfully at filepath %s in session with %s", filePath, sess.serverAddr)
	}
	return err
}

func (sess *ClientSession) DeleteLock(filePath api.FilePath) error {
	if sess.jeopardyFlag {
		durationJeopardyOver := time.Until(sess.startTime.Add(sess.leaseLength + JeopardyDuration))
		select {
		case <-sess.jeopardyChan:
			sess.logger.Printf("session with %s reestablished", sess.serverAddr)
		case <-time.After(durationJeopardyOver):
			return errors.New(fmt.Sprintf("session with %s expired", sess.serverAddr))
		}
	}
	sess.logger.Printf("Sending DeleteLock request to server %s", sess.serverAddr)
	req := api.DeleteLockRequest{ClientID: sess.clientID, FilePath: filePath}
	resp := &api.DeleteLockResponse{}

	var err error
	for { // If we get a connection problem, keep trying.
		err = sess.rpcClient.Call("Handler.DeleteLock", req, resp)
		if err != io.ErrUnexpectedEOF {
			break
		}
	}
	if err != nil {
		sess.logger.Printf("DeleteLock with server %s failed with error %s", sess.serverAddr, err.Error())
	} else {
		sess.logger.Printf("Delete Lock successfully at filepath %s in session with %s", filePath, sess.serverAddr)
	}
	return err
}

func (sess *ClientSession) TryAcquireLock(filePath api.FilePath, mode api.LockMode) (bool, error) {
	if sess.jeopardyFlag {
		durationJeopardyOver := time.Until(sess.startTime.Add(sess.leaseLength + JeopardyDuration))
		select {
		case <-sess.jeopardyChan:
			sess.logger.Printf("session with %s reestablished", sess.serverAddr)
		case <-time.After(durationJeopardyOver):
			return false, errors.New(fmt.Sprintf("session with %s expired", sess.serverAddr))
		}
	}
	/*_, ok := sess.locks[filePath]
	if ok {
		return false, errors.New(fmt.Sprintf("Client already owns the lock %s", filePath))
	}*/

	//sess.logger.Printf("Sending TryAcquireLock request to server %s", sess.serverAddr)
	req := api.TryAcquireLockRequest{ClientID: sess.clientID, FilePath: filePath, Mode: mode}
	resp := &api.TryAcquireLockResponse{}

	var err error
	for { // If we get a connection problem, keep trying.
		err = sess.rpcClient.Call("Handler.TryAcquireLock", req, resp)
		if err != io.ErrUnexpectedEOF {
			break
		}
	}

	if resp.IsSuccessful {
		sess.locks[filePath] = mode
	}
	return resp.IsSuccessful, err
}

func (sess *ClientSession) ReleaseLock(filePath api.FilePath) error {
	if sess.jeopardyFlag {
		durationJeopardyOver := time.Until(sess.startTime.Add(sess.leaseLength + JeopardyDuration))
		select {
		case <-sess.jeopardyChan:
			sess.logger.Printf("session with %s reestablished", sess.serverAddr)
		case <-time.After(durationJeopardyOver):
			// return errors.New(fmt.Sprintf("session with %s expired", sess.serverAddr))
			return fmt.Errorf("session with %s expired", sess.serverAddr)
		}
	}
	_, ok := sess.locks[filePath]
	if !ok {
		//return errors.New(fmt.Sprintf("Client does not own the lock %s", filePath))
		return fmt.Errorf("Client does not own the lock %s", filePath)
	}

	//sess.logger.Printf("Sending ReleaseLock request to server %s", sess.serverAddr)
	req := api.ReleaseLockRequest{ClientID: sess.clientID, Filepath: filePath}
	resp := &api.ReleaseLockResponse{}

	var err error
	for { // If we get a connection problem, keep trying.
		err = sess.rpcClient.Call("Handler.ReleaseLock", req, resp)
		if err != io.ErrUnexpectedEOF {
			break
		}
	}

	if err == nil {
		delete(sess.locks, filePath)
	}
	return err
}

func (sess *ClientSession) ReadContent(filePath api.FilePath) (string, error) {
	if sess.jeopardyFlag {
		durationJeopardyOver := time.Until(sess.startTime.Add(sess.leaseLength + JeopardyDuration))
		select {
		case <-sess.jeopardyChan:
			sess.logger.Printf("session with %s reestablished", sess.serverAddr)
		case <-time.After(durationJeopardyOver):
			//return "", errors.New(fmt.Sprintf("session with %s expired", sess.serverAddr))
			return "", fmt.Errorf("session with %s expired", sess.serverAddr)

		}
	}
	_, ok := sess.locks[filePath]
	if !ok {
		//return "", errors.New(fmt.Sprintf("Client does not own the lock %s", filePath))
		return "", fmt.Errorf("client does not own the lock %s", filePath)

	}

	//sess.logger.Printf("Sending ReleaseLock request to server %s", sess.serverAddr)
	req := api.ReadRequest{ClientID: sess.clientID, FilePath: filePath}
	resp := &api.ReadResponse{}

	var err error
	for { // If we get a connection problem, keep trying.
		err = sess.rpcClient.Call("Handler.ReadContent", req, resp)
		if err != io.ErrUnexpectedEOF {
			break
		}
	}

	return resp.Content, err
}

func (sess *ClientSession) WriteContent(filePath api.FilePath, content string) (bool, error) {
	if sess.jeopardyFlag {
		durationJeopardyOver := time.Until(sess.startTime.Add(sess.leaseLength + JeopardyDuration))
		select {
		case <-sess.jeopardyChan:
			sess.logger.Printf("session with %s reestablished", sess.serverAddr)
		case <-time.After(durationJeopardyOver):
			return false, errors.New(fmt.Sprintf("session with %s expired", sess.serverAddr))
		}
	}
	_, ok := sess.locks[filePath]
	if !ok {
		return false, errors.New(fmt.Sprintf("Client does not own the lock %s", filePath))
	}

	//sess.logger.Printf("Sending ReleaseLock request to server %s", sess.serverAddr)
	req := api.WriteRequest{ClientID: sess.clientID, FilePath: filePath, Content: content}
	resp := &api.WriteResponse{}

	var err error
	for { // If we get a connection problem, keep trying.
		err = sess.rpcClient.Call("Handler.WriteContent", req, resp)
		if err != io.ErrUnexpectedEOF {
			break
		}
	}

	return resp.IsSuccessful, err
}

func (sess *ClientSession) IsExpired() bool {
	return sess.expired
}

// --------------------------------------------------------------------------------------------
// ADDED FUNCTIONS BELOW FROM NITHYA & CHEE KIAT CLIENT SIDE 7 NOV 140AM
// TODO MODIFY SYNTAX SO THAT THE FUNCTIONS BELOW CAN BE USED OR UPDATE CORRESPONDING FUNCTIONS ABOVE

// // Request Lock of type write for a particular seat
// func (s *ClientSession) RequestLock(seatID string, clientID string) error {
// 	s.mutex.Lock()
// 	defer s.mutex.Unlock()
// 	//checks if seatID is found in the map s.Locks
// 	//ok is true if found and false if not found
// 	lockMode, ok := s.locks[seatID]
// 	//if lock exists and is a writelock return error
// 	if ok && lockMode.Type == WriteLock {
// 		return fmt.Errorf("Seat %s is currently locked", seatID)
// 	}
// 	req := &Request{SeatID: seatID, ClientID: clientID}
// 	resp := new(Response)
// 	err := s.rpcClient.Call(serverHandler.RequestLock, req, resp)
// 	if err != nil || !resp.Success {
// 		return fmt.Errorf("Unable to require lock on seat %s", seatID)
// 	}
// 	// If successful, add the write lock to the local map.
// 	s.locks[seatID] = LockMode{Type: WriteLock}
// 	s.logger.Printf("Lock aquired for seat %s by client ID %s", seatID, clientID)
// 	return nil
// }

// // Release Lock of type write for a particular seat if client changes mind
// func (s *Session) ReleaseLock(seatID string, clientID string) error {
// 	s.mutex.Lock()
// 	defer s.mutex.Unlock()
// 	//checks if seatID is found in the map s.Locks
// 	//ok is true if found and false if not found
// 	lockMode, ok := s.locks[seatID]
// 	//if no write lock is found for the seat return error
// 	if !ok || lockMode.Type != WriteLock {
// 		return fmt.Errorf("Unable to find write lock for seat %s ", seatID)
// 	}
// 	req := &Request{SeatID: seatID, ClientID: clientID}
// 	resp := new(Response)
// 	err := s.rpcClient.Call(serverHandler.ReleaseLock, req, resp)
// 	if err != nil || !resp.Success {
// 		return fmt.Errorf("Unable to release lock on seat %s", seatID)
// 	}
// 	// If successful, remove the write lock from the local map.
// 	delete(s.locks, seatID)
// 	s.logger.Printf("Lock released for seat %s by client ID %s", seatID, clientID)
// 	return nil
// }

// // Delete Lock of type write for a particular seat if client chooses seat
// func (s *Session) DeleteLock(seatID string, clientID string) error {
// 	s.mutex.Lock()
// 	defer s.mutex.Unlock()
// 	req := &Request{SeatID: seatID, ClientID: clientID}
// 	resp := new(Response)
// 	err := s.rpcClient.Call(serverHandler.DeleteLock, req, resp)
// 	if err != nil || !resp.Success {
// 		return fmt.Errorf("Unable to delete lock for seat %s", seatID)
// 	}
// 	// If successful, remove the write lock from the local map.
// 	delete(s.locks, seatID)
// 	s.logger.Printf("Lock deleted for seat %s by client ID %s", seatID, clientID)
// 	return nil
// }

// func (client *Client) ReserveSeat(seatID string, clientId string) {

// 	// ask chubby session to request write lock
// 	// if denied write lock access, chubby session send failure message ("seat %s is currently reserved, booked, or unavailable", seatID)
// 	// client will do hot reload on that particular seat to show reserved

// 	// if successful, chubby session will store write lock, and send success acknowledgement back to client
// 	// client then updates seat status to reserved in DB and update UI in seat status on webpage
// }

// func (client *Client) BookSeat(seatID string, clientID string) {
// 	client.mutex.Lock()

// 	//go through all reserved seats
// 	//update DB of reserved seats to booked
// 	//send lock release

// }
