package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

type RaftNode struct {
	Server        *Server
	State         ServerState
	CurrentTerm   int
	VotedFor      int
	VoteCount     int
	ElectionTimer *time.Timer
	HeartbeatChan chan bool
	QuitChan      chan bool
	Mutex         sync.Mutex // Standard mutex for concurrency control
}

func NewRaftNode(server *Server) *RaftNode {
	fmt.Println("Test NewRaftNode")
	rn := &RaftNode{
		Server:        server,
		State:         Follower,
		CurrentTerm:   0,
		VotedFor:      -1,
		HeartbeatChan: make(chan bool),
		QuitChan:      make(chan bool),
	}
	rn.resetElectionTimer()
	return rn
}

func (rn *RaftNode) resetElectionTimer() {
	if rn.State == Leader {
		// Optionally, do not reset the election timer for the leader
		return
	}
	timeout := time.Duration(ElectionTimeoutMin+rand.Intn(ElectionTimeoutMax-ElectionTimeoutMin)) * time.Millisecond
	if rn.ElectionTimer != nil {
		rn.ElectionTimer.Stop()
	}
	rn.ElectionTimer = time.NewTimer(timeout)
}

func (rn *RaftNode) Run() {
	go rn.electionTimeoutHandler()
	counter := 0

	fmt.Println("Server", rn.Server.ID, " RaftNode/Run")
	for {
		select {
		case <-rn.HeartbeatChan:
			counter++
			if counter > 5 {
				fmt.Println("Server", rn.Server.ID, "counter is greater than 5")
				return
			}
			rn.resetElectionTimer()

		case _, ok := <-rn.QuitChan:
			if !ok {
				fmt.Println("Server", rn.Server.ID, "quitting RaftNode/Run")
				return
			}
		}
	}
}

func (rn *RaftNode) electionTimeoutHandler() {
	for {
		select {
		case <-rn.ElectionTimer.C:
			rn.Mutex.Lock()
			if rn.State != Leader {
				rn.startElection()
			}
			rn.resetElectionTimer()
			rn.Mutex.Unlock()
		case _, ok := <-rn.QuitChan:
			if !ok {
				return
			}
		}
	}
}

func (rn *RaftNode) startElection() {
	rn.State = Candidate
	rn.CurrentTerm += 1
	rn.VotedFor = rn.Server.ID
	rn.VoteCount = 1

	fmt.Printf("Server %d starting election for term %d\n", rn.Server.ID, rn.CurrentTerm)

	for _, peer := range rn.Server.Peers {
		go func(peer *Server) {
			select {
			case _, ok := <-rn.QuitChan:
				if !ok {
					fmt.Printf("Server %d election goroutine to peer %d exiting\n", rn.Server.ID, peer.ID)
					return
				}
			default:
				voteGranted := peer.RaftNode.handleRequestVote(RequestVote{
					Term:        rn.CurrentTerm,
					CandidateID: rn.Server.ID,
				})
				if voteGranted {
					rn.Mutex.Lock()
					rn.VoteCount += 1
					if rn.VoteCount > len(rn.Server.Peers)/2 && rn.State == Candidate {
						rn.State = Leader
						fmt.Printf("Server %d became leader for term %d\n", rn.Server.ID, rn.CurrentTerm)
						go rn.sendHeartbeats()
					}
					rn.Mutex.Unlock()
				}
			}
		}(peer)
	}
	// No need to reset election timer here; it's handled by the caller
}

func (rn *RaftNode) handleRequestVote(args RequestVote) bool {
	rn.Mutex.Lock()
	defer rn.Mutex.Unlock()

	if args.Term < rn.CurrentTerm {
		return false
	}
	if args.Term > rn.CurrentTerm {
		rn.CurrentTerm = args.Term
		rn.VotedFor = -1
		rn.State = Follower
	}
	if rn.VotedFor == -1 || rn.VotedFor == args.CandidateID {
		rn.VotedFor = args.CandidateID
		rn.resetElectionTimer()
		return true
	}
	return false
}
func (rn *RaftNode) sendHeartbeats() {
	for {
		select {
		case _, ok := <-rn.QuitChan:
			if !ok {
				fmt.Printf("Server %d exiting sendHeartbeats\n", rn.Server.ID)
				return
			}
		default:
			rn.Mutex.Lock()
			if rn.State != Leader {
				rn.Mutex.Unlock()
				return
			}
			rn.Mutex.Unlock()

			for _, peer := range rn.Server.Peers {
				go func(peer *Server) {
					select {
					case _, ok := <-rn.QuitChan:
						if !ok {
							fmt.Printf("Server %d stopping heartbeat to peer %d\n", rn.Server.ID, peer.ID)
							return
						}
					default:
						peer.RaftNode.receiveHeartbeat(rn.CurrentTerm)
					}
				}(peer)
			}
			time.Sleep(HeartbeatInterval)
		}
	}
}

func (rn *RaftNode) receiveHeartbeat(term int) {
	rn.Mutex.Lock()
	defer rn.Mutex.Unlock()

	if term >= rn.CurrentTerm {
		rn.CurrentTerm = term
		rn.State = Follower
		rn.resetElectionTimer()
		rn.HeartbeatChan <- true
	}
}
