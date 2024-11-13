package main

import "time"

type ServerState int
type LockMode int

const (
	Follower ServerState = iota
	Candidate
	Leader

	Free LockMode = iota
	Reserved
	Booked

	DefaultLeaseDuration = 20 * time.Second
	JeopardyDuration     = 45 * time.Second
)

type RequestVote struct {
	Term        int
	CandidateID int
}

type RequestVoteReply struct {
	Term        int
	VoteGranted bool
}

type LockRequest struct {
	ClientID string
	LockName string
}

type LockResponse struct {
	Success bool
	Message string
}

type RequestLockArgs struct {
	SeatID   string
	ClientID string
}

type RequestLockResponse struct {
	Success bool
	Message string
}
