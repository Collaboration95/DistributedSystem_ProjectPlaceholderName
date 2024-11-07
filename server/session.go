// defines chubby session metadata, and operations on locks that can
// be performed as part of a Chubby session

package server

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/rpc"
	"sync"
	"time"

	"github.com/Collaboration95/DistributedSystem_ProjectPlaceholderName.git/api"
)

const DefaultLeaseExt = 15 * time.Second

// Session contains metadata for one Chubby session
// for simplicity, we say that each client can only init one session with
// the chubby servers

type Session struct {
	// client to which this session corresponds
	clientID api.ClientID

	// Start time
	startTime time.Time

	// Length of the Lease
	leaseLength time.Duration

	rpcClient *rpc.Client //use rpc package to establish session server connection

	// TTL Lock
	ttlLock sync.Mutex

	// channel used to block KeepAlive
	ttlChannel chan struct{}

	// a data structure describing which locks the client holds
	// maps lock filepath -> Lock struct
	locks map[api.FilePath]*Lock

	// Did we terminate this session?
	terminated bool

	// terminated channel
	terminatedChan chan struct{}
	mutex          sync.Mutex // protext session operations
}

// lock describes information about a particular chubby lock
type Lock struct {
	path    api.FilePath          // path to this lock in the store
	mode    api.LockMode          // api.FREE or exclusive lock?
	owners  map[api.ClientID]bool // who is holding the lock?
	content string                // the content of the file

}

// create session struct
func CreateSession(clientID api.ClientID) (*Session, error) {
	sess, ok := app.sessions[clientID]
	if ok {
		return nil, errors.New(fmt.Sprintf("The client already has a session established with the master"))
	}
	app.logger.Printf("Creating session with client %s", clientID)

	// create new session struct
	sess = &Session{
		clientID:       clientID,
		startTime:      time.Now(),
		leaseLength:    DefaultLeaseExt,
		ttlChannel:     make(chan struct{}, 2),
		locks:          make(map[api.FilePath]*Lock),
		terminated:     false,
		terminatedChan: make(chan struct{}, 2),
	}

	// add the session to the sessions map
	app.sessions[clientID] = sess

	// in a separate goroutine, periodically check if the lease is over
	go sess.MonitorSession()

	return sess, nil

}

func (sess *Session) MonitorSession() {
	app.logger.Printf("Monitoring session with client %s", sess.clientID)

	// at each second, check time until the lease is over
	ticker := time.Tick(time.Second)
	for range ticker {
		timeLeaseOver := sess.startTime.Add(sess.leaseLength)

		var durationLeaseOver time.Duration = 0
		if timeLeaseOver.After(time.Now()) {
			durationLeaseOver = time.Until(timeLeaseOver)
		}

		if durationLeaseOver == 0 {
			// lease expired: terminate this session
			app.logger.Printf("Lease with client %s expired: terminating session", sess.clientID)
			sess.TerminateSession()
			return
		}
		if durationLeaseOver <= (1 * time.Second) {
			// trigger KeepAlive response 1 second before timeout
			sess.ttlChannel <- struct{}{}
		}
	}

}

// terminate the session
func (sess *Session) TerminateSession() {
	// we cannot delete the session from the app session map bcos
	// chubby could have experienced a failover event

	sess.terminated = true
	close(sess.terminatedChan)

	// release all the locks in the session
	for filePath := range sess.locks {
		err := sess.ReleaseLock(filePath)
		if err != nil {
			app.logger.Printf(
				"error when client %s releasing lock at %s:%s",
				sess.clientID,
				filePath,
				err.Error())
		}

	}

	app.logger.Printf("terminated session with client %s", sess.clientID)
}

// extend lease after receiving keepAlive messages
func (sess *Session) KeepAlive(clientID api.ClientID) time.Duration {
	// block until shortly before lease expires
	select {
	case <-sess.terminatedChan:
		// return early response saying that session should end
		return sess.leaseLength

	case <-sess.ttlChannel:
		// extend lease by 12 seconds
		sess.leaseLength = sess.leaseLength + DefaultLeaseExt

		app.logger.Printf(
			"session with client %s extended: lease length %s",
			sess.clientID,
			sess.leaseLength.String())

		// return new lease length
		return sess.leaseLength
	}

}

// // create the lock if it does not exist
// func (sess *Session) OpenLock(path api.FilePath) error {
// 	// check if lock exists in persistent store
// 	_, err := app.store.Get(string(path))
// 	if err != nil {
// 		// Add lock to persistent store: (key: LockPath, value: "")
// 		err = app.store.Set(string(path), "")
// 		if err != nil {
// 			return err
// 		}
// 		// add lock to in-memory struct of locks
// 		lock := &Lock{
// 			path:    path,
// 			mode:    api.FREE,
// 			owners:  make(map[api.ClientID]bool),
// 			content: "",
// 		}
// 		app.locks[path] = lock
// 		sess.locks[path] = lock
// 	}
// 	return nil
// }

// delete the lock. lock must be held in exclusive mode before calling DeleteLock
func (sess *Session) DeleteLock(path api.FilePath) error {
	// If we are not holding the lock, we cannot delete it.
	lock, exists := sess.locks[path]
	if !exists {
		//return errors.New(fmt.Sprintf("Client does not hold the lock at path %s", path))
		return fmt.Errorf("client does not hold the lock at path %s", path)
	}
	// check if we are holding the lock in exclusive mode
	if lock.mode != api.EXCLUSIVE {
		// return errors.New(fmt.Sprintf("Client does not hold the lock at path %s in exclusive mode", path))
		return fmt.Errorf("client does not hold the lock at path %s in exclusive mode", path)
	}

	// check that the lock actually exists in the store
	_, err := app.store.Get(string(path))

	if err != nil {
		// return errors.New(fmt.Sprintf("Lock at %s does not exist in persistent store", path))
		return fmt.Errorf("Lock at %s does not exist in persistent store", path)

	}
	// delete the lock from session metadata
	delete(sess.locks, path)

	// delete the lock from in-memory struct of locks
	delete(app.locks, path)

	// delete the lock from the store
	err = app.store.Delete(string(path))
	return err

}

// try to reserve a seat for the client (when user clicks on seat)
// try to acquire lock, returning either success (true) or failure (false)
// func (sess *Session) TryAcquireLock(seatID string, path api.FilePath, mode api.LockMode) (bool, error) {
// 	args := api.TryAcquireLockRequest{SeatID: seatID, ClientID: sess.clientID}

// 	// // validate mode of the lock
// 	// if mode != api.EXCLUSIVE && mode != api.SHARED {
// 	// 	return false, errors.New(fmt.Sprintf("Invalid mode."))
// 	// }

// 	// do we already own the lock? fail with error

// 	// check if lock exists in persistent store
// 	_, err := app.store.Get(string(path))

// 	if err != nil {
// 		return false, errors.New(fmt.Sprintf("Lock at %s has not been opened", path))
// 	}

// 	// check if lock exists in in-mem struct
// 	lock, exists := app.locks[path]
// 	if !exists {
// 		// assume some failure has occured
// 		// recover lock struct: add lock to in-memory struct of locks
// 		app.logger.Printf("Lock does not exist wuth Client ID", sess.clientID)

// 		lock = &Lock{
// 			path:    path,
// 			mode:    api.FREE,
// 			owners:  make(map[api.ClientID]bool),
// 			content: "",
// 		}
// 		app.locks[path] = lock
// 		sess.locks[path] = lock
// 	}

// 	// check the mode of the lock
// 	switch lock.mode {
// 	case api.EXCLUSIVE:
// 		// should fail: someone probably already owns the lock
// 		if len(lock.owners) == 0 {
// 			// throw an error if there are no owners but lock.mode is api.EXCLUSIVE:
// 			// this means ReleaseLock was not implemented correctly
// 			return false, errors.New("Lock has EXCLUSIVE mode despite having no owners")

// 		} else if len(lock.owners) > 1 {
// 			return false, errors.New("Lock has EXCLUSIVE mode despite having multiple owners")
// 		} else {
// 			// fail with no error
// 			app.logger.Printf("Failed to acquire lock %s: already held in EXCLUSIVE mode", path)
// 			return false, nil
// 		}

// 	// case api.SHARED:
// 	// 	// IF MODE IS api.SHARED, then succeed; else fail
// 	// 	if mode == api.EXCLUSIVE {
// 	// 		app.logger.Printf("Failed to acquire lock %s in EXCLUSIVE mode: already held in SHARED mode", path)
// 	// 		return false, nil
// 	// 	} else { // mode == api.SHARED
// 	// 		// update lock owners
// 	// 		lock.owners[sess.clientID] = true

// 	// 		// add lock to session lock struct
// 	// 		sess.locks[path] = lock
// 	// 		sess.locks[path] = lock
// 	// 		// return success
// 	// 		// app.logger.Printf("Lock %s acquired successfully with mode SHARED", path)
// 	// 		return true, nil
// 	// 	}
// 	case api.FREE:
// 		// if lock has owners, either TryAcquireLock or ReleaseLock was not implemented correctly
// 		if len(lock.owners) > 0 {
// 			return false, errors.New("Lock has FREE mode but is owned by 1 or more clients")

// 		}
// 		// should succeed regardless of mode
// 		// update lock owners
// 		lock.owners[sess.clientID] = true

// 		// update lock mode
// 		lock.mode = mode

// 		// add lock to session lock struct
// 		sess.locks[path] = lock

// 		// update lock mode in the global map
// 		app.locks[path] = lock

// 		// Return success
// 		//if mode == api.SHARED {
// 		//	app.logger.Printf("Lock %s acquired successfully with mode SHARED", path)
// 		//} else {
// 		//	app.logger.Printf("Lock %s acquired successfully with mode EXCLUSIVE", path)
// 		//}

//			return true, nil
//		default:
//			return false, errors.New(fmt.Sprintf("Lock at %s has undefined mode %d", path, lock.mode))
//		}
//	}
//
// TryAcquireLock tries to reserve a seat for the client (when user clicks on seat)
// and attempts to acquire a lock, returning success (true) or failure (false).
func (sess *Session) TryAcquireLock(seatID string, path api.FilePath, mode api.LockMode) (bool, error) {
	args := api.TryAcquireLockRequest{SeatID: seatID, ClientID: sess.clientID, FilePath: path, Mode: mode}

	// Validate lock mode
	if mode != api.EXCLUSIVE && mode != api.FREE {
		return false, errors.New("invalid lock mode")
	}

	// Check if the lock exists in persistent storage
	_, err := app.store.Get(string(path))
	if err != nil {
		// return false, errors.New(fmt.Sprintf("Lock at %s has not been opened", path))
		return false, fmt.Errorf("Lock at %s has not been opened", path)
	}

	// Check if lock exists in the in-memory struct
	lock, exists := app.locks[path]
	if !exists {
		// Recover lock struct by adding it to in-memory locks
		app.logger.Printf("Lock does not exist for Client ID %s", sess.clientID)
		lock = &Lock{
			path:    path,
			mode:    api.FREE,
			owners:  make(map[api.ClientID]bool),
			content: "",
		}
		app.locks[path] = lock
		sess.locks[path] = lock
	}

	// Check the mode of the lock
	switch lock.mode {
	case api.EXCLUSIVE:
		// Fail if someone already owns the lock exclusively
		if len(lock.owners) > 0 {
			app.logger.Printf("Failed to acquire lock %s: already held in EXCLUSIVE mode", path)
			return false, nil
		}

	case api.FREE:
		// Grant the lock exclusively if in FREE mode
		lock.owners[sess.clientID] = true
		lock.mode = api.EXCLUSIVE
		sess.locks[path] = lock
		app.locks[path] = lock

		app.logger.Printf("Lock %s acquired successfully in EXCLUSIVE mode", path)
		// Now perform the RPC call to inform the server of the acquired lock
		resp := new(api.TryAcquireLockResponse)
		err = sess.rpcClient.Call("server.Handler.TryAcquireLock", args, resp)
		if err != nil {
			// If the RPC call fails, return false and the error
			return false, fmt.Errorf("RPC call failed to inform server: %v", err)
		}

		// If the RPC call was successful and the lock is acquired, return true
		if resp.IsSuccessful {
			return true, nil
		} else {
			// If the server indicates failure, rollback the local lock acquisition
			lock.owners[sess.clientID] = false
			lock.mode = api.FREE
			sess.locks[path] = lock
			app.locks[path] = lock

			return false, errors.New("server failed to confirm lock acquisition")
		}
		// return true, nil

	default:
		// return false, errors.New(fmt.Sprintf("Lock at %s has undefined mode %d", path, lock.mode))
		return false, fmt.Errorf("Lock at %s has undefined mode %d", path, lock.mode)
	}
	// If none of the cases matched, return false (this is a safe fallback)
	return false, nil
} // TODO FIX THIS

// RELEASE THE LOCK
func (sess *Session) ReleaseLock(path api.FilePath) error {
	// check if lock exists in persistent store
	_, err := app.store.Get(string(path))

	if err != nil {
		// return errors.New(fmt.Sprintf("Client with id %s: Lock at %s does not exist in persistent store", path, sess.clientID))
		return fmt.Errorf("client with id %s: Lock at %s does not exist in persistent store", path, sess.clientID)
	}

	// grab lock struct from session locks map
	lock, present := app.locks[path]

	// if not in session locks map, throw an error
	if !present || lock == nil {
		// return errors.New(fmt.Sprintf("Lock at %s does not exist in session locks map", path))
		return fmt.Errorf("lock at %s does not exist in session locks map", path)
	}

	// switch on lock mode
	switch lock.mode {
	case api.FREE:
		// throw an error: this means TryAcquire was not implemented correctly
		// return errors.New(fmt.Sprint("Lock at %s has FREE mode: acquire not implemented correctly Client ID %s", path, sess.clientID))
		return fmt.Errorf("Lock at %s has FREE mode: acquire not implemented correctly Client ID %s", path, sess.clientID)
	case api.EXCLUSIVE:
		// delete lock from owners
		delete(lock.owners, sess.clientID)

		// set lock mode
		lock.mode = api.FREE

		// delete lock from session locks map
		delete(sess.locks, path)
		app.locks[path] = lock
		log.Printf("Release lock at %s\n", path)
		// return without error
		return nil
	// case api.SHARED:
	// 	// delete from lock owners
	// 	delete(lock.owners, sess.clientID)

	// 	// set lock mode if no more owners
	// 	if len(lock.owners) == 0 {
	// 		lock.mode = api.FREE
	// 	}

	// 	// delete lock from session locks map
	// 	delete(sess.locks, path)
	// 	app.locks[path] = lock

	// 	// return without error
	// 	return nil
	default:
		// return errors.New(fmt.Sprintf("Lock at %s has undefined mode %d", path, lock.mode))
		return fmt.Errorf("Lock at %s has undefined mode %d", path, lock.mode)

	}
}

// read the content from a lockfile
func (sess *Session) ReadContent(path api.FilePath) (string, error) {
	// check if file exists in persistent store
	content, err := app.store.Get(string(path))

	if err != nil {
		// return "", errors.New(fmt.Sprintf("Client with id %s: File at %s does not exist in persistent store", path, sess.clientID))
		return "", fmt.Errorf("client with id %s: File at %s does not exist in persistent store", sess.clientID, path)
	}

	// grab lock struct from session locks map
	lock, present := app.locks[path]

	// if not in session locks map, throw an error
	if !present || lock == nil {
		// return "", errors.New(fmt.Sprintf("Lock at %s does not exist in session locks map", path))
		return "", fmt.Errorf("lock at %s does not exist in session locks map", path)
	}

	// check that we are among the owners of the lock
	_, present = lock.owners[sess.clientID]
	if !present || !lock.owners[sess.clientID] {
		// return "", errors.New(fmt.Sprintf("Client %d does not own lock at path %s", sess.clientID, path))
		return "", fmt.Errorf("client %s does not own lock at path %s", sess.clientID, path)
	}
	return content, nil
}

// write the content to a lockfile
func (sess *Session) WriteContent(path api.FilePath, content string) error {
	// check if file exists in persistent store
	_, err := app.store.Get(string(path))

	if err != nil {
		// return errors.New(fmt.Sprintf("Client with id %s: File at %s does not exist in persistent store", path, sess.clientID))
		return fmt.Errorf("client with id %s: File at %s does not exist in persistent store", path, sess.clientID)
	}

	// grab lock struct from session locks map
	lock, present := app.locks[path]

	// if not in session locks map, throw an error
	if !present || lock == nil {
		// return errors.New(fmt.Sprintf("Lock at %s does not exist in session locks map", path))
		return fmt.Errorf("Lock at %s does not exist in session locks map", path)
	}

	// check that we are among the owners of the lock
	_, present = lock.owners[sess.clientID]
	if !present || !lock.owners[sess.clientID] {
		// return errors.New(fmt.Sprintf("client %d does not own lock at path %s", sess.clientID, path))
		return fmt.Errorf("client %s does not own lock at path %s", sess.clientID, path)
	}

	err = app.store.Set(string(path), content)
	if err != nil {
		// return errors.New(fmt.Sprintf("Write Error"))
		return fmt.Errorf("write error")
	}
	return nil

}

// BELOW FUNCTIONS FROM NOVAN INTERMEDIATE BRANCH
// --------------------------------------------------------------------------
// ChubbyNode represents a single server in the Chubby cell
type ChubbyNode struct {
	isLeader  bool
	Seats     map[string]api.LockMode // Mapping of seat IDs to their lock status
	locks     map[string]string       // Maps seat IDs to the client who holds the lock
	lockLimit int                     // Maximum locks allowed per client
	mutex     sync.Mutex
	clients   map[string]time.Time // Tracks last active time for clients
}

// NewChubbyNode initializes a new ChubbyNode
func NewChubbyNode(isLeader bool) *ChubbyNode {
	node := &ChubbyNode{
		isLeader:  isLeader,
		Seats:     make(map[string]api.LockMode),
		locks:     make(map[string]string),
		lockLimit: 5,                          // Limit clients to 5 simultaneous locks
		clients:   make(map[string]time.Time), // Initialize client tracking
	}
	// Initialize 50 seats
	for i := 1; i <= 50; i++ {
		node.Seats[fmt.Sprintf("seat-%d", i)] = api.FREE
	}
	return node
}

// RequestLock handles lock requests from clients tries to reserve a seat for client
func (node *ChubbyNode) RequestLock(args api.TryAcquireLockRequest, reply *api.TryAcquireLockResponse) error {
	node.mutex.Lock()
	defer node.mutex.Unlock()

	seatID := args.SeatID
	clientID := args.ClientID

	// Check if the seat is already reserved or booked (in exclusive mode)
	if currentStatus, exists := node.Seats[seatID]; exists && currentStatus == api.EXCLUSIVE {
		reply.IsSuccessful = false
		//reply.Message = fmt.Sprintf("%s is already booked or unavailable", seatID)
		return nil
	}

	// // Check if seat is reserved by another client
	// if lockHolder, exists := node.locks[seatID]; exists && lockHolder != clientID {
	// 	reply.Success = false
	// 	reply.Message = fmt.Sprintf("%s is already reserved by another client", seatID)
	// 	return nil
	// }

	// Reserve the seat and assign lock to the client
	node.Seats[seatID] = api.EXCLUSIVE
	node.locks[seatID] = string(clientID) // TODO FIX THIS
	reply.IsSuccessful = true
	//reply.Message = fmt.Sprintf("%s reserved successfully", seatID)
	return nil
}

// BookSeat confirms the reservation by marking a seat as booked
func (node *ChubbyNode) BookSeat(args *api.TryAcquireLockRequest, reply *api.TryAcquireLockResponse) error {
	node.mutex.Lock()
	defer node.mutex.Unlock()

	seatID := args.SeatID
	clientID := args.ClientID

	// // Ensure the seat is currently reserved by the requesting client
	// if currentStatus, exists := node.Seats[seatID]; !exists || currentStatus != Reserved || node.locks[seatID] != clientID {
	// 	reply.IsSuccessful = false
	// 	//reply.Message = fmt.Sprintf("%s is not reserved by you or is unavailable", seatID)
	// 	return nil
	// }

	// Ensure the seat is currently reserved by the requesting client
	if currentStatus, exists := node.Seats[seatID]; !exists || currentStatus != api.EXCLUSIVE || node.locks[seatID] != string(clientID) {
		reply.IsSuccessful = false
		return nil
	}

	// Book the seat
	node.Seats[seatID] = api.EXCLUSIVE
	delete(node.locks, seatID)
	reply.IsSuccessful = true
	//reply.Message = fmt.Sprintf("%s booked successfully", seatID)
	return nil
}

// ReleaseLock releases a lock on a seat if it's in an exclusive state
func (node *ChubbyNode) ReleaseLock(args api.TryAcquireLockRequest, reply api.TryAcquireLockResponse) error {
	node.mutex.Lock()
	defer node.mutex.Unlock()

	seatID := args.SeatID
	clientID := args.ClientID

	// // Ensure the seat is currently reserved by the requesting client
	// if currentStatus, exists := node.Seats[seatID]; !exists || currentStatus != Reserved || node.locks[seatID] != clientID {
	// 	reply.Success = false
	// 	reply.Message = fmt.Sprintf("%s is not reserved by you or is unavailable", seatID)
	// 	return nil
	// }

	// Ensure the seat is currently reserved by the requesting client
	if lockHolder, exists := node.locks[seatID]; !exists || lockHolder != string(clientID) {
		reply.IsSuccessful = false
		return nil
	}

	// Release the lock and set the seat to FREE mode
	node.Seats[seatID] = api.FREE
	delete(node.locks, seatID)
	reply.IsSuccessful = true
	//reply.Message = fmt.Sprintf("%s lock released", seatID)
	return nil
}

// // KeepAlive handles keep-alive requests from clients
// func (node *ChubbyNode) KeepAlive(args KeepAliveArgs, reply *KeepAliveResponse) error {
// 	node.mutex.Lock()
// 	defer node.mutex.Unlock()

// 	// Update the last active time for the client
// 	node.clients[args.ClientID] = time.Now()
// 	reply.Success = true
// 	reply.Message = "Keep-alive acknowledged"
// 	return nil
// }

// // StartServer initializes and runs the Chubby server on the specified port
// func StartServer(port string, isLeader bool) error {
// 	node := NewChubbyNode(isLeader)

// 	rpc.Register(node)
// 	listener, err := net.Listen("tcp", ":"+port)
// 	if err != nil {
// 		return fmt.Errorf("failed to start server on port %s: %v", port, err)
// 	}
// 	defer listener.Close()

// 	log.Printf("ChubbyNode started on port %s (Leader: %v)", port, isLeader)

// 	for {
// 		conn, err := listener.Accept()
// 		if err != nil {
// 			log.Printf("connection accept error: %v", err)
// 			continue
// 		}
// 		go rpc.ServeConn(conn)
// 	}
// }

// func main() {
// 	// Start 5 nodes with one leader
// 	var wg sync.WaitGroup
// 	ports := []string{"8000", "8001", "8002", "8003", "8004"}

// 	for i, port := range ports {
// 		wg.Add(1)
// 		isLeader := i == 0 // First node is the leader
// 		go func(port string, isLeader bool) {
// 			defer wg.Done()
// 			if err := StartServer(port, isLeader); err != nil {
// 				log.Fatalf("Server failed on port %s: %v", port, err)
// 			}
// 		}(port, isLeader)
// 	}

// 	wg.Wait()
// }

// SREE DEVI ADDED 7 NOV 1230 PM
// Server struct representing the server
type Server struct {
	locks map[api.FilePath]api.LockMode
}

// TryAcquireLock method that handles client lock acquisition requests
func (s *Server) TryAcquireLock(req *api.TryAcquireLockRequest, resp *api.TryAcquireLockResponse) error {
	// Simulate lock acquisition logic
	if s.locks[req.FilePath] == api.EXCLUSIVE {
		resp.IsSuccessful = false
		return nil
	}

	s.locks[req.FilePath] = req.Mode
	resp.IsSuccessful = true
	return nil
}

// StartServer method to start the server and listen for RPC calls
func StartServer(address string) {
	server := &Server{
		locks: make(map[api.FilePath]api.LockMode),
	}

	rpc.Register(server)

	listener, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatal("Error starting server:", err)
	}

	fmt.Println("Server started at", address)
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Fatal("Error accepting connection:", err)
		}
		go rpc.ServeConn(conn)
	}
}
