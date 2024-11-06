// defines chubby session metadata, and operations on locks that can
// be performed as part of a Chubby session

package server

import (
	"errors"
	"fmt"
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
}

// lock describes information about a particular chubby lock
type Lock struct {
	path    api.FilePath          // path to this lock in the store
	mode    api.LockMode          // api.SHARED or exclusive lock?
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

// create the lock if it does not exist
func (sess *Session) OpenLock(path api.FilePath) error {
	// check if lock exists in persistent store
	_, err := app.store.Get(string(path))
	if err != nil {
		// Add lock to persistent store: (key: LockPath, value: "")
		err = app.store.Set(string(path), "")
		if err != nil {
			return err
		}
		// add lock to in-memory struct of locks
		lock := &Lock{
			path:    path,
			mode:    api.FREE,
			owners:  make(map[api.ClientID]bool),
			content: "",
		}
		app.locks[path] = lock
		sess.locks[path] = lock
	}
	return nil
}

// delete the lock. lock must be held in exclusive mode before calling DeleteLock
func (sess *Session) DeleteLock(path api.FilePath) error {
	// If we are not holding the lock, we cannot delete it.
	lock, exists := sess.locks[path]
	if !exists {
		return errors.New(fmt.Sprintf("Client does not hold the lock at path %s", path))
	}
	// check if we are holding the lock in exclusive mode
	if lock.mode != api.EXCLUSIVE {
		return errors.New(fmt.Sprintf("Client does not hold the lock at path %s in exclusive mode", path))
	}

	// check that the lock actually exists in the store
	_, err := app.store.Get(string(path))

	if err != nil {
		return errors.New(fmt.Sprintf("Lock at %s does not exist in persistent store", path))

	}
	// delete the lock from session metadata
	delete(sess.locks, path)

	// delete the lock from in-memory struct of locks
	delete(app.locks, path)

	// delete the lock from the store
	err = app.store.Delete(string(path))
	return err

}

// try to acquire lock, returning either success (true) or failure (false)
func (sess *Session) TryAcquireLock(path api.FilePath, mode api.LockMode) (bool, error) {
	// validate mode of the lock
	if mode != api.EXCLUSIVE && mode != api.SHARED {
		return false, errors.New(fmt.Sprintf("Invalid mode."))
	}

	// do we already own the lock? fail with error

	// check if lock exists in persistent store
	_, err := app.store.Get(string(path))

	if err != nil {
		return false, errors.New(fmt.Sprintf("Lock at %s has not been opened", path))
	}

	// check if lock exists in in-mem struct
	lock, exists := app.locks[path]
	if !exists {
		// assume some failure has occured
		// recover lock struct: add lock to in-memory struct of locks
		app.logger.Printf("Lock does not exist wuth Client ID", sess.clientID)

		lock = &Lock{
			path:    path,
			mode:    api.FREE,
			owners:  make(map[api.ClientID]bool),
			content: "",
		}
		app.locks[path] = lock
		sess.locks[path] = lock
	}

	// check the mode of the lock
	switch lock.mode {
	case api.EXCLUSIVE:
		// should fail: someone probably already owns the lock
		if len(lock.owners) == 0 {
			// throw an error if there are no owners but lock.mode is api.EXCLUSIVE:
			// this means ReleaseLock was not implemented correctly
			return false, errors.New("Lock has EXCLUSIVE mode despite having no owners")

		} else if len(lock.owners) > 1 {
			return false, errors.New("Lock has EXCLUSIVE mode despite having multiple owners")
		} else {
			// fail with no error
			app.logger.Printf("Failed to acquire lock %s: already held in EXCLUSIVE mode", path)
			return false, nil
		}

	case api.SHARED:
		// IF MODE IS api.SHARED, then succeed; else fail
		if mode == api.EXCLUSIVE {
			app.logger.Printf("Failed to acquire lock %s in EXCLUSIVE mode: already held in SHARED mode", path)
			return false, nil
		} else { // mode == api.SHARED
			// update lock owners
			lock.owners[sess.clientID] = true

			// add lock to session lock struct
			sess.locks[path] = lock
			sess.locks[path] = lock
			// return success
			// app.logger.Printf("Lock %s acquired successfully with mode SHARED", path)
			return true, nil
		}
	case api.FREE:
		// if lock has owners, either TryAcquireLock or ReleaseLock was not implemented correctly
		if len(lock.owners) > 0 {
			return false, errors.New("Lock has FREE mode but is owned by 1 or more clients")

		}
		// should succeed regardless of mode
		// update lock owners
		lock.owners[sess.clientID] = true

		// update lock mode
		lock.mode = mode

		// add lock to session lock struct
		sess.locks[path] = lock

		// update lock mode in the global map
		app.locks[path] = lock

		// Return success
		//if mode == api.SHARED {
		//	app.logger.Printf("Lock %s acquired successfully with mode SHARED", path)
		//} else {
		//	app.logger.Printf("Lock %s acquired successfully with mode EXCLUSIVE", path)
		//}

		return true, nil
	default:
		return false, errors.New(fmt.Sprintf("Lock at %s has undefined mode %d", path, lock.mode))
	}
}

// RELEASE THE LOCK
