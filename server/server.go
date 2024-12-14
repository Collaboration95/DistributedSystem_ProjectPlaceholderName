package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/rpc"
	"os"
	"rpc-system/common"
	"strings"
	"sync"
	"time"

	"golang.org/x/exp/rand"
)

const (
	seatFile      = "seats.txt"
	numServers    = 5
	timeout       = 10 * time.Second
	serverTimeout = 3 * time.Second
	interval      = 1 * time.Second
	maxTimeout    = 1500 * time.Millisecond
	minTimeout    = 500 * time.Millisecond
)

type InternalMessage struct {
	SourceId int
	Type     string
	Data     interface{}
}

type RaftState int

const (
	Follower RaftState = iota
	Candidate
	Leader
)

var loadBalancer = &LoadBalancer{}

var localSeats = make(map[string]Seat)

type LoadBalancer struct {
	LeaderPort string
	LeaderID   string // Add the serverID of the leader
}

type Seat struct {
	SeatID   string
	Status   string
	ClientID string
}

// LOG REPLICATION LogEntry STRUCT //
type LogEntry struct {
	Index    int
	Term     int
	Command  interface{}
	ClientID string
}

type Server struct {
	// Role and state information
	role          RaftState
	term          int
	votes         int
	votedFor      int
	lastHeartbeat time.Time
	isLeader      bool
	isAlive       bool
	// Networking and communication
	LeaderPort string
	LocalPort  string
	serverID   int
	OutgoingCh []chan InternalMessage
	IncomingCh chan InternalMessage

	sessions map[string]struct {
		requestCh   chan common.Request
		keepaliveCh chan common.Request
	}
	requests  chan common.Request
	responses map[string]chan common.Response

	seats    map[string]Seat
	filePath string

	mu           sync.Mutex
	sessionMux   sync.Mutex
	keepaliveMux sync.Mutex

	// LOG REPLICATION SERVER STRUCT //
	replicatedCount int
	log             []LogEntry
	commitIndex     int
	lastApplied     int
	nextIndex       []int
	matchIndex      []int
	logMutex        sync.RWMutex
}

func init_Server(numServer int, filePath string) []*Server {
	servers := make([]*Server, numServer)
	for i := 0; i < numServer; i++ {
		servers[i] = &Server{
			LocalPort: fmt.Sprintf(":%d", 12346+i),
			serverID:  i,
			role:      Follower,
			term:      0,
			votedFor:  -1,
			sessions: make(map[string]struct {
				requestCh   chan common.Request
				keepaliveCh chan common.Request
			}),
			IncomingCh: make(chan InternalMessage, 100),
			OutgoingCh: make([]chan InternalMessage, numServer),
			requests:   make(chan common.Request, 100), // Global processing queue
			responses:  make(map[string]chan common.Response),
			seats:      make(map[string]Seat),
			filePath:   filePath,
			isAlive:    true,
			votes:      0,

			// LOG REPLICATION SERVER INIT //
			replicatedCount: 1,
			log:             []LogEntry{},
			commitIndex:     -1,
			lastApplied:     -1,
			nextIndex:       make([]int, numServer),
			matchIndex:      make([]int, numServer),
		}

		// Init nextIndex
		for j := 0; j < numServer; j++ {
			servers[i].nextIndex[j] = 0
		}

		// Load the seat data from the file
		servers[i].seats = localSeats
	}

	// Connect each server to every other server
	for i, server := range servers {
		for j, otherServer := range servers {
			if i != j {
				server.OutgoingCh[j] = otherServer.IncomingCh
			}
		}
	}
	return servers
}

// START LOG REPLICATION FUNC //

// Add a method to validate the request before logging
func (s *Server) validateRequest(req common.Request) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	seatID := strings.ToUpper(strings.TrimSpace(req.SeatID))
	if seat, exists := s.seats[seatID]; exists {
		switch req.Type {
		case "RESERVE":
			if seat.Status == "available" {
				return "SUCCESS"
			}
			return "FAILURE: Seat already occupied"
		case "CANCEL":
			if seat.Status == "occupied" {
				requestingClientID := getClientIDfromSessionID(req.ClientID)
				occupyingClientID := getClientIDfromSessionID(seat.ClientID)
				if requestingClientID == occupyingClientID {
					return "SUCCESS"
				}
				return "FAILURE: Seat occupied by different client"
			}
			return "FAILURE: Seat not occupied"
		default:
			return "FAILURE: Invalid request type"
		}
	}
	return "FAILURE: Seat does not exist"
}

func (s *Server) ReplicateLog(entry LogEntry) error {
	fmt.Printf("Server %d (Term %d) trying to send log\n", s.serverID, s.term)

	// First, validate the request
	status := s.validateRequest(entry.Command.(common.Request))
	if status != "SUCCESS" {
		return fmt.Errorf("request validation failed: %s", status)
	}

	// Append the log entry first
	s.logMutex.Lock()
	entry.Index = len(s.log)
	entry.Term = s.term
	s.log = append(s.log, entry)
	s.logMutex.Unlock()

	LogEntry := InternalMessage{
		SourceId: s.serverID,
		Type:     "APPENDENTRIES",
		Data: map[string]interface{}{
			"term":         s.term,
			"leaderId":     s.serverID,
			"prevLogIndex": entry.Index - 1,
			"prevLogTerm":  s.term, // Assuming same term for simplicity
			"entries":      []LogEntry{entry},
			"leaderCommit": s.commitIndex,
		},
	}

	// Send log to all other servers
	for i := range s.OutgoingCh {
		if i == s.serverID {
			continue // Skip sending heartbeat to itself
		}
		select {
		case s.OutgoingCh[i] <- LogEntry:
			// log sent successfully
		default:
			fmt.Printf("Failed to send LogEntry to server %d (channel might be full)\n", i)
		}
	}
	return nil
}

// Handle AppendEntries from leader
func (s *Server) handleAppendEntries(msg InternalMessage) {
	data := msg.Data.(map[string]interface{})
	leaderTerm := data["term"].(int)
	leaderID := data["leaderId"].(int)
	prevLogIndex := data["prevLogIndex"].(int)
	prevLogTerm := data["prevLogTerm"].(int)
	entries := data["entries"].([]LogEntry)
	leaderCommit := data["leaderCommit"].(int)

	// Reply false if term < currentTerm
	if leaderTerm < s.term {
		// Send negative response
		responseMsg := InternalMessage{
			SourceId: s.serverID,
			Type:     "APPENDENTRIESRESPONSE",
			Data: map[string]interface{}{
				"term":    s.term,
				"success": false,
			},
		}
		s.OutgoingCh[leaderID] <- responseMsg
		return
	}

	// Update term and convert to follower
	if leaderTerm > s.term {
		s.term = leaderTerm
		s.role = Follower
		s.votedFor = -1
	}

	// Check log consistency
	if prevLogIndex >= 0 && prevLogIndex < len(s.log) {
		if s.log[prevLogIndex].Term != prevLogTerm {
			// Send negative response if log doesn't match
			responseMsg := InternalMessage{
				SourceId: s.serverID,
				Type:     "APPENDENTRIESRESPONSE",
				Data: map[string]interface{}{
					"term":    s.term,
					"success": false,
				},
			}
			s.OutgoingCh[leaderID] <- responseMsg
			return
		}
	}

	// Append new entries
	s.logMutex.Lock()
	for _, entry := range entries {
		if entry.Index >= len(s.log) {
			s.log = append(s.log, entry)
		} else if s.log[entry.Index].Term != entry.Term {
			// If existing entry conflicts with new entry, delete existing entries
			s.log = s.log[:entry.Index]
			s.log = append(s.log, entry)
		}
	}
	s.logMutex.Unlock()

	// Update commit index
	if leaderCommit > s.commitIndex {
		s.commitIndex = min(leaderCommit, len(s.log)-1)
	}

	// Send success response
	responseMsg := InternalMessage{
		SourceId: s.serverID,
		Type:     "APPENDENTRIESRESPONSE",
		Data: map[string]interface{}{
			"term":    s.term,
			"success": true,
		},
	}
	s.OutgoingCh[leaderID] <- responseMsg
}

func (s *Server) applyLogEntries() {
	s.logMutex.Lock()
	defer s.logMutex.Unlock()

	// Ensure we don't try to access log entries that don't exist
	if s.commitIndex >= len(s.log) {
		s.commitIndex = len(s.log) - 1
	}

	for s.lastApplied < s.commitIndex {
		s.lastApplied++

		// Double-check to prevent index out of range  *** THERE IS AN ERROR TRIGGERED HERE ***
		if s.lastApplied >= len(s.log) {
			fmt.Printf("Warning: Last applied %d is greater than log length %d\n", s.lastApplied, len(s.log))
			break
		}

		entry := s.log[s.lastApplied]

		// Apply the log entry to the state machine (seat reservation/cancellation)
		switch cmd := entry.Command.(type) {
		case common.Request:
			switch cmd.Type {
			case "RESERVE":
				s.seats[cmd.SeatID] = Seat{
					SeatID:   cmd.SeatID,
					Status:   "occupied",
					ClientID: cmd.ClientID,
				}
			case "CANCEL":
				s.seats[cmd.SeatID] = Seat{
					SeatID:   cmd.SeatID,
					Status:   "available",
					ClientID: "",
				}
			}
		}
	}

	// Save updated seats to persistent storage
	s.saveSeats()
}

// LOG REPLICATION FUNC END //

func (s *Server) CreateSession(clientID string, reply *string) error {
	s.sessionMux.Lock()
	defer s.sessionMux.Unlock()

	sessionID := fmt.Sprintf("server-session-%s", clientID)

	// Create channels for the session
	requestCh := make(chan common.Request, 10)   // Regular requests
	keepaliveCh := make(chan common.Request, 10) // Keepalives
	s.sessions[sessionID] = struct {
		requestCh   chan common.Request
		keepaliveCh chan common.Request
	}{
		requestCh:   requestCh,
		keepaliveCh: keepaliveCh,
	}

	// Start the server session
	go s.serverSession(sessionID, requestCh, keepaliveCh)

	*reply = fmt.Sprintf("Session %s created for client %s", sessionID, clientID)
	log.Printf("Created %s for client %s", sessionID, clientID)
	return nil
}

func (s *Server) serverSession(sessionID string, requestCh chan common.Request, keepaliveCh chan common.Request) {
	log.Printf("Server session %s started", sessionID)

	timer := time.NewTimer(timeout)

	for {
		select {
		case req := <-keepaliveCh:
			if req.Type == "KEEPALIVE" {
				log.Printf("Session %s received KeepAlive from %s", sessionID, req.ClientID)

				// Reset the timer on a KeepAlive
				if !timer.Stop() {
					<-timer.C
				}
				timer.Reset(timeout)
			}

		case req := <-requestCh:
			log.Printf("Session %s received request: %+v", sessionID, req)

			// Forward the request to the global queue
			s.requests <- req

			// Reset the timer on a valid request
			if !timer.Stop() {
				<-timer.C
			}
			timer.Reset(timeout)

		case <-timer.C:
			// Timer fired, meaning no activity within the timeout period
			log.Printf("Session %s timed out. Cleaning up...", sessionID)

			// Clean up the session and exit the goroutine
			s.sessionMux.Lock()
			delete(s.sessions, sessionID)
			s.sessionMux.Unlock()
			close(requestCh)
			close(keepaliveCh)
			return
		}
	}
}

func (s *Server) ProcessRequest(req *common.Request, res *common.Response) error {
	s.sessionMux.Lock()
	session, exists := s.sessions[req.ServerID]
	s.sessionMux.Unlock()

	if !exists {
		res.Status = "FAILURE"
		res.Message = fmt.Sprintf("Session %s does not exist", req.ServerID)
		return nil
	}

	if req.Type == "KEEPALIVE" {
		session.keepaliveCh <- *req
		res.Status = "SUCCESS"
		res.Message = "KeepAlive acknowledged"
		return nil
	}

	if s.role != Leader {
		res.Status = "FAILURE"
		res.Message = "Not the Leader. Please redirect to the leader"
		return nil
	}

	// LOG REPLICATION METHOD START //

	// Validate request before creating log entry
	validationStatus := s.validateRequest(*req)
	if validationStatus != "SUCCESS" {
		res.Status = "FAILURE"
		res.Message = validationStatus
		return nil
	}

	// Create a log entry for the request
	logEntry := LogEntry{
		Command:  *req,
		ClientID: req.ClientID,
	}

	// Attempt to replicate the log
	err := s.ReplicateLog(logEntry)
	if err != nil {
		res.Status = "FAILURE"
		res.Message = fmt.Sprintf("Log replication failed: %v", err)
		return nil
	}

	// LOG REPLICATION METHOD END //

	requestID := fmt.Sprintf("%s-%d", req.ClientID, time.Now().UnixNano())
	responseCh := make(chan common.Response, 1)

	s.mu.Lock()
	s.responses[requestID] = responseCh
	s.mu.Unlock()

	req.ClientID = requestID
	session.requestCh <- *req

	response := <-responseCh
	res.Status = response.Status
	res.Message = response.Message

	s.mu.Lock()
	delete(s.responses, requestID)
	s.mu.Unlock()

	res.Status = "SUCCESS"
	res.Message = fmt.Sprintf("Operation %s for seat %s processed successfully", req.Type, req.SeatID)
	return nil
}

func (s *Server) SendHeartbeats() {
	fmt.Printf("Server %d (Term %d) sending heartbeats\n", s.serverID, s.term)

	message := InternalMessage{
		SourceId: s.serverID,
		Type:     "HEARTBEAT",
		Data:     fmt.Sprintf("Heartbeat from Leader %d", s.serverID),
	}

	// Send heartbeat to all other servers
	for i := range s.OutgoingCh {
		if i == s.serverID {
			continue // Skip sending heartbeat to itself
		}
		select {
		case s.OutgoingCh[i] <- message:
			// Heartbeat sent successfully
		default:
			fmt.Printf("Failed to send heartbeat to server %d (channel might be full)\n", i)
		}
	}
}

func (s *Server) runServerLoop() {
	// Prepare the heartbeat ticker
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	isFirstRound := true
	lastHeartbeat := time.Now()

	// Main combined loop: handles requests, heartbeats, and incoming messages
	for {
		select {
		case req := <-s.requests:
			// Handle requests (formerly processQueue logic)
			responseMessage := ""
			status := "SUCCESS"

			s.mu.Lock()
			seatID := strings.ToUpper(strings.TrimSpace(req.SeatID)) // Normalize SeatID
			if seat, exists := s.seats[seatID]; exists {
				switch req.Type {
				case "RESERVE":
					if seat.Status == "available" {
						s.seats[seatID] = Seat{
							SeatID:   req.SeatID,
							Status:   "occupied",
							ClientID: req.ClientID,
						}
						responseMessage = fmt.Sprintf("Seat %s reserved for client %s by %s", seatID, req.ClientID, req.ServerID)
						//s.saveSeats() // Save the updated seat map
					} else {
						status = "FAILURE"
						responseMessage = fmt.Sprintf("Seat %s is already occupied. Client %s by %s", seatID, req.ClientID, req.ServerID)
					}
				case "CANCEL":
					if seat.Status == "occupied" {
						requestingClientID := getClientIDfromSessionID(req.ClientID)
						occupyingClientID := getClientIDfromSessionID(seat.ClientID)
						if requestingClientID != occupyingClientID {
							status = "FAILURE"
							responseMessage = fmt.Sprintf("Seat %s is occupied by another client %s. Cannot cancel", seatID, seat.ClientID)
							break
						}
						s.seats[seatID] = Seat{
							SeatID:   req.SeatID,
							Status:   "available",
							ClientID: "",
						}
						responseMessage = fmt.Sprintf("Reservation for seat %s cancelled by client %s via %s", seatID, req.ClientID, req.ServerID)
						//s.saveSeats() // Save the updated seat map
					} else {
						status = "FAILURE"
						responseMessage = fmt.Sprintf("Seat %s is not occupied. Cannot cancel. Client %s by %s", seatID, req.ClientID, req.ServerID)
					}
				default:
					status = "FAILURE"
					responseMessage = fmt.Sprintf("Invalid request type %s for seat %s by client %s", req.Type, seatID, req.ClientID)
				}
			} else {
				status = "FAILURE"
				responseMessage = fmt.Sprintf("Seat %s does not exist. Client %s by %s", seatID, req.ClientID, req.ServerID)
			}
			s.mu.Unlock()

			log.Printf("[%s] %s", status, responseMessage)

			if responseCh, exists := s.responses[req.ClientID]; exists {
				responseCh <- common.Response{Status: status, Message: responseMessage}
			}

		case <-ticker.C:
			// This block was originally in handleHeartbeats
			if !s.isAlive {
				continue
			}

			if s.role == Leader {
				// Leader sends heartbeats
				s.SendHeartbeats()
			} else {
				if isFirstRound {
					// Skip timeout and start election immediately on the first tick
					isFirstRound = false
					fmt.Printf("Server %d skipping timeout and starting immediate election (Term %d)\n", s.serverID, s.term+1)

					s.term += 1
					s.role = Candidate
					s.votedFor = s.serverID
					s.votes = 1 // vote for itself

					requestVoteMessage := InternalMessage{
						SourceId: s.serverID,
						Type:     "REQUESTVOTE",
						Data:     s.term,
					}

					// Broadcast REQUESTVOTE to all other servers
					for i := range s.OutgoingCh {
						if i != s.serverID {
							s.OutgoingCh[i] <- requestVoteMessage
						}
					}

					// Reset the lastHeartbeat to prevent immediate re-election
					lastHeartbeat = time.Now()
					continue
				}

				// This server is a follower or candidate
				localDelay := time.Duration(rand.Intn(int(maxTimeout-minTimeout))) + minTimeout

				if time.Since(lastHeartbeat) > (serverTimeout + localDelay) {
					fmt.Printf("servertimeout + localDelay is %v\n", serverTimeout+localDelay)
					// No heartbeat received within timeout: start election
					log.Printf("Server %d: No heartbeat received. Starting election (Term %d -> Term %d)", s.serverID, s.term, s.term+1)

					// Step 2: Become a candidate
					s.term += 1
					s.role = Candidate
					s.votedFor = s.serverID
					s.votes = 1 // vote for itself
					fmt.Printf("\nServer %d ENTERING CANDIDATE TIME \n\n", s.serverID)
					// Send RequestVote RPC to other servers
					requestVoteMessage := InternalMessage{
						SourceId: s.serverID,
						Type:     "REQUESTVOTE",
						Data:     s.term,
					}

					// Broadcast REQUESTVOTE to all other servers
					for i := range s.OutgoingCh {
						if i != s.serverID {
							s.OutgoingCh[i] <- requestVoteMessage
						}
					}
					// resetting heartbeat
					lastHeartbeat = time.Now()
				}
			}

		case msg := <-s.IncomingCh:
			// Handle incoming cluster messages (originally in handleHeartbeats)
			if !s.isAlive {
				continue
			}

			switch msg.Type {
			case "HEARTBEAT":
				fmt.Printf("Server %d received %s from Server %d\n", s.serverID, msg.Type, msg.SourceId)
				lastHeartbeat = time.Now()

				if s.role == Candidate {
					s.role = Follower
					s.votedFor = -1
				}

			case "REQUESTVOTE":
				candidateID := msg.SourceId
				candidateTerm := msg.Data.(int)

				if candidateTerm > s.term {
					s.term = candidateTerm
					s.role = Follower
					s.votedFor = candidateID
					fmt.Printf("Server %d granted vote to Server %d for Term %d\n", s.serverID, candidateID, candidateTerm)

					voteResponse := InternalMessage{
						SourceId: s.serverID,
						Type:     "VOTERESPONSE",
						Data: map[string]interface{}{
							"term":        s.term,
							"voteGranted": true,
						},
					}
					s.OutgoingCh[candidateID] <- voteResponse

				} else if candidateTerm == s.term {
					if s.votedFor == -1 || s.votedFor == candidateID {
						s.votedFor = candidateID
						fmt.Printf("Server %d granted vote to Server %d for Term %d\n", s.serverID, candidateID, candidateTerm)

						voteResponse := InternalMessage{
							SourceId: s.serverID,
							Type:     "VOTERESPONSE",
							Data: map[string]interface{}{
								"term":        s.term,
								"voteGranted": true,
							},
						}
						s.OutgoingCh[candidateID] <- voteResponse
					} else {
						fmt.Printf("Server %d denied vote to Server %d for Term %d (already voted)\n", s.serverID, candidateID, candidateTerm)

						voteResponse := InternalMessage{
							SourceId: s.serverID,
							Type:     "VOTERESPONSE",
							Data: map[string]interface{}{
								"term":        s.term,
								"voteGranted": false,
							},
						}
						s.OutgoingCh[candidateID] <- voteResponse
					}

				} else {
					fmt.Printf("Server %d denied vote to Server %d for Term %d (stale term, current Term %d)\n", s.serverID, candidateID, candidateTerm, s.term)

					voteResponse := InternalMessage{
						SourceId: s.serverID,
						Type:     "VOTERESPONSE",
						Data: map[string]interface{}{
							"term":        s.term,
							"voteGranted": false,
						},
					}
					s.OutgoingCh[candidateID] <- voteResponse
				}

			case "VOTERESPONSE":
				if s.role == Candidate {
					respData := msg.Data.(map[string]interface{})
					responseTerm := respData["term"].(int)
					voteGranted := respData["voteGranted"].(bool)

					if responseTerm > s.term {
						fmt.Printf("Server %d received VOTERESPONSE with higher term %d. Stepping down to Follower.\n", s.serverID, responseTerm)
						s.term = responseTerm
						s.role = Follower
						s.votedFor = -1
						return
					}

					if responseTerm == s.term && voteGranted {
						s.votes++
						fmt.Printf("Server %d received vote. Total votes: %d\n", s.serverID, s.votes)

						if s.votes > len(s.OutgoingCh)/2 {
							s.role = Leader
							s.isLeader = true
							s.votedFor = -1
							s.SendHeartbeats()
							fmt.Printf("Server %d became Leader for Term %d\n", s.serverID, s.term)
							update_LoadBalancer(s.LocalPort, fmt.Sprintf("Server%d", s.serverID))
						}
					} else {
						// Negative or stale vote response ignored
					}
				}
			// LOG REPLICATION CASES //
			case "APPENDENTRIES":
				s.handleAppendEntries(msg)

			case "APPENDENTRIESRESPONSE":
				respData := msg.Data.(map[string]interface{})
				successValue, ok := respData["success"].(bool)
				if ok && successValue {
					s.replicatedCount++
				}

				if s.role == Leader {
					// If majority of servers have replicated the log
					if s.replicatedCount > len(s.OutgoingCh)/2 {
						s.commitIndex++
						fmt.Printf("Log entry successfully replicated to majority of servers. New Commit Index: %d, Term: %d\n",
							s.commitIndex, s.term)

						// Apply the committed log entries
						s.applyLogEntries()
					}
				}
			}
		}
	}
}

func (lb *LoadBalancer) GetLeaderIP(req *common.Request, res *common.Response) error {
	fmt.Println("Load balancer received request for leader info, sending back leader details")
	res.Status = "SUCCESS"
	res.Message = fmt.Sprintf("%s,%s", lb.LeaderPort, lb.LeaderID) // Return port and ID
	return nil
}

func init_LoadBalancer(servers []*Server) *LoadBalancer {
	lb := &LoadBalancer{}
	for i, server := range servers {
		if server.isLeader {
			lb.LeaderPort = server.LocalPort
			lb.LeaderID = fmt.Sprintf("Server%d", i)
		}
	}
	return lb
}

func update_LoadBalancer(LeaderPort string, LeaderID string) {
	fmt.Printf("Updating LoadBalancer with new leader info: %s, %s\n", LeaderPort, LeaderID)
	loadBalancer.LeaderPort = LeaderPort
	loadBalancer.LeaderID = LeaderID
}

func main() {
	// Load the seat data from the file
	loadSeats(seatFile)

	errLoadBalancer := rpc.Register(loadBalancer)
	if errLoadBalancer != nil {
		log.Fatalf("Error registering load balancer: %s", errLoadBalancer)
	}

	// Start the load balancer listener
	lbListener, errlb := net.Listen("tcp", ":12345")
	if errlb != nil {
		log.Fatalf("Error starting load balancer listener: %s", errlb)
	}
	defer lbListener.Close()
	log.Println("LoadBalancer is running on port 12345")

	// Start a goroutine to handle load balancer requests
	go func() {
		for {
			conn, err := lbListener.Accept()
			if err != nil {
				log.Println("Load Balancer connection error:", err)
				continue
			}
			go rpc.ServeConn(conn)
		}
	}()

	// Give the load balancer some time to initialize
	time.Sleep(2 * time.Second)

	// Initialize servers
	servers := init_Server(numServers, seatFile)

	// Register and start each server
	for _, server := range servers {
		// Register the server with a unique name
		serverName := fmt.Sprintf("Server%d", server.serverID)
		err := rpc.RegisterName(serverName, server)
		if err != nil {
			log.Fatalf("Error registering %s: %s", serverName, err)
		}

		go func(s *Server) {
			listener, err := net.Listen("tcp", s.LocalPort)
			if err != nil {
				log.Fatalf("Error starting %s on port %s: %s", serverName, s.LocalPort, err)
			}
			defer listener.Close()
			log.Printf("%s is running on port %s\n", serverName, s.LocalPort)

			for {
				conn, err := listener.Accept()
				if err != nil {
					log.Printf("%s connection error: %s", serverName, err)
					continue
				}
				go rpc.ServeConn(conn)
			}
		}(server)
	}

	for _, server := range servers {
		go server.runServerLoop()
		// LOG REPLICATION PRINTING //
		go server.periodicLogPrinting()
		// go server.handleHeartbeats()
	}

	// Prevent main from exiting
	select {}
}

// helper functions ( not relevant to flow of code)
func getClientIDfromSessionID(sessionID string) string {
	return strings.Split(sessionID, "-")[0]
}

func loadSeats(filePath string) {
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("Error opening file %s: %s", filePath, err)

	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var seats []Seat

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		parts := strings.Split(line, ":")
		if len(parts) == 3 {
			seatID := strings.ToUpper(strings.TrimSpace(parts[0]))
			status := strings.TrimSpace(parts[1])
			clientID := strings.TrimSpace(parts[2])

			seat := Seat{
				SeatID:   seatID,
				Status:   status,
				ClientID: clientID,
			}
			seats = append(seats, seat)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatalf("Error reading file %s: %s", filePath, err)
	}

	localSeats = make(map[string]Seat)
	for _, seat := range seats {
		localSeats[seat.SeatID] = seat
	}
	log.Println("Seats loaded from file.")
}

// saveSeats writes the current seat map to a file
func (s *Server) saveSeats() error {
	file, err := os.OpenFile(s.filePath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file for saving seats: %w", err)
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	for seatID, seat := range s.seats {
		_, err := fmt.Fprintf(writer, "%s: %s: %s\n", seatID, seat.Status, seat.ClientID)
		if err != nil {
			return fmt.Errorf("failed to write seat data to file: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush seat data to file: %w", err)
	}
	log.Println("Seats successfully updated in file.")
	return nil
}

// LOG REPLICATION //
// PrintServerLog prints the current state of the server's log
func (s *Server) PrintServerLog() {
	roleStr := "Follower"
	if s.role == Leader {
		roleStr = "LEADER"
	} else if s.role == Candidate {
		roleStr = "Candidate"
	}

	fmt.Printf("\n===== SERVER %d LOG (Term %d, Role: %s) =====\n", s.serverID, s.term, roleStr)

	s.logMutex.RLock()
	defer s.logMutex.RUnlock()

	if len(s.log) == 0 {
		fmt.Println("Log is empty")
		return
	}

	fmt.Println("Index | Term | Command Type | Client ID")
	fmt.Println("------|------|--------------|----------")

	for _, entry := range s.log {
		cmdType := "UNKNOWN"
		clientID := "N/A"

		// Try to extract command type and client ID
		if req, ok := entry.Command.(common.Request); ok {
			cmdType = req.Type
			clientID = req.ClientID
		}

		fmt.Printf("%5d | %4d | %12s | %s\n",
			entry.Index,
			entry.Term,
			cmdType,
			clientID)
	}

	fmt.Printf("Commit Index: %d\n", s.commitIndex)
	fmt.Println("============================================\n")
}

// Add a method to periodically print logs
func (s *Server) periodicLogPrinting() {
	ticker := time.NewTicker(10 * time.Second)
	for {
		select {
		case <-ticker.C:
			s.PrintServerLog()

			// If this is a follower, show last received entries
			if s.role != Leader {
				fmt.Println("Last Received Log Entries:")
				for i := max(0, len(s.log)-5); i < len(s.log); i++ {
					entry := s.log[i]
					fmt.Printf("Index: %d, Term: %d, Command: %v\n",
						entry.Index, entry.Term, entry.Command)
				}
			}
		}
	}
}

// Helper function to get the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Helper function to get the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
