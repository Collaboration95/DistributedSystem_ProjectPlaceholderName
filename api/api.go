// interfaces for RPC calls between clients and servers

package api

import "time"

/*
* Shared types
 */

type ClientID string
type FilePath string

// mode of a lock
type LockMode int

//exclusive is write mode - user selects seat: reserves or books seats
// shared is for viewing seats
// free is for no lock [tentative]

// const (
// 	EXCLUSIVE LockMode = iota
// 	SHARED
// 	FREE
// )
const (
	EXCLUSIVE LockMode = iota
	FREE
)

/*
* RPC Interfaces
 */

type InitSessionRequest struct {
	ClientID ClientID
}

type InitSessionResponse struct {
	LeaseLength time.Duration // Lease length for the session
	Success     bool          // Whether initialization was successful
	Error       string        // Any error message from the server
}

type KeepAliveRequest struct {
	ClientID ClientID
	// session information:
	Locks map[FilePath]LockMode //Locks held by the client
}

type KeepAliveResponse struct {
	LeaseLength time.Duration
	Success     bool
}

// from nithya & chee kiat - not using this
// type LockMode struct {
// 	Type string // either read or write
// }

// //lock types as a constant
// const (
// 	ReadLock  = "read"
// 	WriteLock = "write"
// )

type OpenLockRequest struct {
	ClientID ClientID
	FilePath FilePath
}

type OpenLockResponse struct {
}

type DeleteLockRequest struct {
	ClientID ClientID
	FilePath FilePath
}

type DeleteLockResponse struct {
}

type TryAcquireLockRequest struct {
	SeatID   string // added this
	ClientID ClientID
	FilePath FilePath
	Mode     LockMode
}

type TryAcquireLockResponse struct {
	IsSuccessful bool
}

type ReleaseLockRequest struct {
	ClientID ClientID
	Filepath FilePath
}

type ReleaseLockResponse struct {
}

type ReadRequest struct {
	ClientID ClientID
	FilePath FilePath
}

type ReadResponse struct {
	Content string
}

type WriteRequest struct {
	ClientID ClientID
	FilePath FilePath
	Content  string
}

type WriteResponse struct {
	IsSuccessful bool
}
