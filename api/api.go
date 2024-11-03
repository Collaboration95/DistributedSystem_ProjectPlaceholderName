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

const (
	EXCLUSIVE LockMode = iota
	SHARED
	FREE
)

/*
* RPC Interfaces
 */

type InitSessionRequest struct {
	ClientID ClientID
}

type InitSessionResponse struct {
}

type KeepAliveRequest struct {
	ClientID ClientID
	// session information:
	Locks map[FilePath]LockMode //Locks held by the client
}

type KeepAliveResponse struct {
	LeaseLength time.Duration
}

// TODO : make all fields exported

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
