package main

type ServerState int

const (
	Follower ServerState = iota
	Candidate
	Leader
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
