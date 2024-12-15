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
	numServers    = 3
	timeout       = 15 * time.Second
	serverTimeout = 3 * time.Second
	interval      = 1 * time.Second
	maxTimeout    = 1500 * time.Millisecond
	minTimeout    = 500 * time.Millisecond
)

type ReplicationState struct {
	originalRequest common.Request
	responseCh      chan common.Response
	successCount    int
	totalFollowers  int
	committed       bool
}

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

type LogEntry struct {
	Term    int
	Command common.Request
}

type Seat struct {
	SeatID   string
	Status   string
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
	pendingReplications map[int]*ReplicationState
	LeaderPort          string
	LocalPort           string
	serverID            int
	OutgoingCh          []chan InternalMessage
	IncomingCh          chan InternalMessage

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

	logEntries     []LogEntry
	commitIndex    int
	lastApplied    int
	RaftOutgoingch []chan InternalMessage
	RaftIncomingch chan InternalMessage
}

type AppendEntries struct {
	Term         int
	LeaderId     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogEntry
	LeaderCommit int
}

type AppendCommit struct {
	Term         int
	LeaderId     int
	LeaderCommit int
}

type AppendCommitResponse struct {
	Term    int  // The current term of the responding follower
	Success bool // Indicates whether the commit acknowledgment was successful
}

type AppendEntriesResponse struct {
	Term    int
	Success bool
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
			IncomingCh:          make(chan InternalMessage, 100),
			OutgoingCh:          make([]chan InternalMessage, numServer),
			requests:            make(chan common.Request, 100), // Global processing queue
			responses:           make(map[string]chan common.Response),
			pendingReplications: make(map[int]*ReplicationState),
			seats:               make(map[string]Seat),
			filePath:            filePath,
			isAlive:             true,
			votes:               0,
			logEntries:          []LogEntry{}, // Initialize empty log
			commitIndex:         0,
			lastApplied:         0,
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

func (s *Server) applyCommand(req common.Request) (string, string) {
	responseMessage := ""
	status := "SUCCESS"

	s.mu.Lock()
	defer s.mu.Unlock()

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
				s.saveSeats() // Save the updated seat map
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
				s.saveSeats() // Save the updated seat map
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

	return status, responseMessage
}

func (s *Server) snapshot() {
	s.mu.Lock()
	defer s.mu.Unlock()

	fmt.Println("\n--- Snapshot of Server State ---")
	fmt.Printf("Server ID: %d\n", s.serverID)
	fmt.Printf("Role: %v\n", s.role)
	fmt.Printf("Term: %d\n", s.term)
	fmt.Printf("Voted For: %d\n", s.votedFor)
	fmt.Printf("Votes: %d\n", s.votes)
	fmt.Printf("Is Leader: %v\n", s.isLeader)
	fmt.Printf("Commit Index: %d\n", s.commitIndex)
	fmt.Printf("Last Applied: %d\n", s.lastApplied)
	fmt.Printf("Log Entries: %v\n", s.logEntries)
	fmt.Printf("Number of Sessions: %d\n", len(s.sessions))
	fmt.Printf("Number of Seats: %d\n", len(s.seats))

	fmt.Println("Channels:")
	fmt.Printf("  IncomingCh Length: %d\n", len(s.IncomingCh))
	for i, ch := range s.OutgoingCh {
		fmt.Printf("  OutgoingCh[%d] Length: %d\n", i, len(ch))
	}

	fmt.Printf("  Requests Channel Length: %d\n", len(s.requests))
	fmt.Printf("  Responses Map Length: %d\n", len(s.responses))

	for sessionID, session := range s.sessions {
		fmt.Printf("  Session ID: %s\n", sessionID)
		fmt.Printf("    RequestCh Length: %d\n", len(session.requestCh))
		fmt.Printf("    KeepAliveCh Length: %d\n", len(session.keepaliveCh))
	}

	fmt.Println("Seats:")
	for seatID, seat := range s.seats {
		fmt.Printf("  SeatID: %s, Status: %s, ClientID: %s\n", seatID, seat.Status, seat.ClientID)
	}

	fmt.Println("--- End of Snapshot ---\n")
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
			s.snapshot()
			newEntry := LogEntry{
				Term:    s.term,
				Command: req,
			}

			s.mu.Lock()
			s.logEntries = append(s.logEntries, newEntry)
			prevLogIndex := len(s.logEntries) - 2
			prevLogTerm := 0
			if prevLogIndex >= 0 {
				prevLogTerm = s.logEntries[prevLogIndex].Term
			}
			currentIndex := len(s.logEntries) - 1
			s.mu.Unlock()

			log.Printf("Server %d: Appending new log entry: %+v", s.serverID, newEntry)

			// Prepare AppendEntries message
			appendEntries := AppendEntries{
				Term:         s.term,
				LeaderId:     s.serverID,
				PrevLogIndex: prevLogIndex,
				PrevLogTerm:  prevLogTerm,
				Entries:      []LogEntry{newEntry},
				LeaderCommit: s.commitIndex,
			}

			// Setup replication state
			responseCh := make(chan common.Response, 1)
			s.mu.Lock()
			s.pendingReplications[currentIndex] = &ReplicationState{
				originalRequest: req,
				responseCh:      responseCh,
				successCount:    1, // leader counts as a successful replication
				totalFollowers:  len(s.OutgoingCh),
				committed:       false,
			}
			s.mu.Unlock()

			// Send AppendEntries to all other servers
			for i := range s.OutgoingCh {
				if i == s.serverID {
					continue // Skip sending to self
				}
				go func(serverID int) {
					log.Printf("Server %d: Sending AppendEntries to Server %d", s.serverID, serverID)
					s.OutgoingCh[serverID] <- InternalMessage{
						SourceId: s.serverID,
						Type:     "APPENDENTRIES",
						Data:     appendEntries,
					}
				}(i)
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

		// Incoming heartbeat or vote request
		case msg := <-s.IncomingCh:
			// Handle incoming cluster messages (originally in handleHeartbeats)
			if !s.isAlive {
				continue
			}

			switch msg.Type {

			case "HEARTBEAT":
				// log.Printf("Server %d received %s from Server %d", s.serverID, msg.Type, msg.SourceId)
				lastHeartbeat = time.Now()

				if s.role == Candidate {
					s.role = Follower
					s.votedFor = -1
				}
			case "REQUESTVOTE":
				// Handle vote request (simplified for brevity)
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

			case "APPENDENTRIES":
				data := msg.Data.(AppendEntries)
				s.mu.Lock()
				defer s.mu.Unlock()
				log.Printf("Server %d received AppendEntries from Server %d", s.serverID, data.LeaderId)

				if data.Term < s.term {
					// Reply with failure if the term is outdated
					response := InternalMessage{
						SourceId: s.serverID,
						Type:     "APPENDENTRIESRESPONSE",
						Data: AppendEntriesResponse{
							Term:    s.term,
							Success: false,
						},
					}
					// Send response back to the leader
					s.OutgoingCh[data.LeaderId] <- response
					break
				}
				// tag is this necessary
				if data.Term > s.term {
					s.term = data.Term
					s.role = Follower
					s.votedFor = -1
				}

				if data.PrevLogIndex >= 0 && (data.PrevLogIndex >= len(s.logEntries) || s.logEntries[data.PrevLogIndex].Term != data.PrevLogTerm) {
					// Log mismatch; send failure response
					response := InternalMessage{
						SourceId: s.serverID,
						Type:     "APPENDENTRIESRESPONSE",
						Data: AppendEntriesResponse{
							Term:    s.term,
							Success: false,
						},
					}
					// Send response back to the leader
					s.OutgoingCh[data.LeaderId] <- response
					break
				}

				s.logEntries = append(s.logEntries[:data.PrevLogIndex+1], data.Entries...)

				// Update commit index
				if data.LeaderCommit > s.commitIndex {
					s.commitIndex = min(data.LeaderCommit, len(s.logEntries)-1)
				}
				for s.lastApplied < s.commitIndex {
					s.lastApplied++
					entry := s.logEntries[s.lastApplied]
					req := entry.Command
					s.applyCommand(req)
				}
				s.OutgoingCh[data.LeaderId] <- InternalMessage{
					SourceId: s.serverID,
					Type:     "APPENDENTRIESRESPONSE",
					Data: AppendEntriesResponse{
						Term:    s.term,
						Success: true,
					},
				}

			case "APPENDENTRIESRESPONSE":
				respData := msg.Data.(AppendEntriesResponse)
				s.mu.Lock()
				// Identify which entry this response corresponds to.
				// For simplicity, assume the last sent entry index or track index in AppendEntries itself.
				// Here we'll assume we have the prevLogIndex + len(Entries), which equals currentIndex.
				// In a real implementation, you'd embed the current index in the message or track it differently.
				currentIndex := len(s.logEntries) - 1
				repState, exists := s.pendingReplications[currentIndex]
				if exists && !repState.committed {
					if respData.Success {
						repState.successCount++
						if repState.successCount > repState.totalFollowers/2 {
							// Achieved majority
							s.commitIndex = currentIndex
							entry := s.logEntries[currentIndex]
							status, responseMessage := s.applyCommand(entry.Command)
							log.Printf("[%s] %s", status, responseMessage)

							// Mark as committed and respond to the client
							repState.committed = true
							if responseCh := repState.responseCh; responseCh != nil {
								responseCh <- common.Response{Status: status, Message: responseMessage}
							}

							// Cleanup
							delete(s.pendingReplications, currentIndex)
						}
					} else {
						// If not successful, we may need to retry or handle log inconsistency.
						// For now, just log. A full Raft implementation would backtrack and retry.
						log.Printf("AppendEntries failed from Server %d to Server %d, Term mismatch or log inconsistency.", s.serverID, msg.SourceId)
					}
				}
				s.mu.Unlock()
				// ...

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
