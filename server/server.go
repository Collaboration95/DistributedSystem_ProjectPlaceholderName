package main

import (
	"bufio"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/rpc"
	"os"
	"rpc-system/common"
	"strings"
	"sync"
	"time"
)

// Log Replication //
// define log (struct): index, operation[seatID,  status, clientID], term
// initialise log in server

// TODO
// 1. Client Update (Read global queue [ s.requests]) if write -> appendEntry (add entry to local log of each follower server)
// 1.1 appendEntry to leaderserver -> appendEntry to followers -> followers ConfirmAppend

// 2. -> Leadercommit- > (update seats.txt) ->
// 2.1 when leader receives majority ConfirmAppend -> EntryCommit (edit seat.txt and update own log)
// 2.1 Execute client request

// const (
// 	seatFile      = "seats.txt"
// 	numServers    = 5 // Change to 5
// 	timeout       = 5 * time.Second
// 	serverTimeout = 3 * time.Second
// 	interval      = 2 * time.Second
// 	maxTimeout    = 350 * time.Millisecond
// 	minTimeout    = 150 * time.Millisecond
// )

const (
	seatFile      = "seats.txt"
	numServers    = 5
	timeout       = 5 * time.Second
	serverTimeout = 2 * time.Second
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
type logString string

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

// Log Replication //
type LogEntry struct {
	SeatID    string
	Operation string
	ClientID  string
}

type ConfirmAppend struct {
	ConfirmAppend string
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

	// Log Replication //
	AppendLogEntryCh chan []LogEntry    // for each server's log
	ConfirmAppendCh  chan ConfirmAppend // for server to acknowledge that it has appended entry
	logString        string

	Log map[int]string // {index : logString }
	// Log map[int]map[string]string {index : {seatID: , op: , clientID}}

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

			// Log Replication //
			logString:        "",
			AppendLogEntryCh: make(chan []LogEntry, 100),
			ConfirmAppendCh:  make(chan ConfirmAppend, 100),
			Log:              make(map[int]string),
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
	go s.serverSession(sessionID, requestCh, keepaliveCh, 10*time.Second)

	*reply = fmt.Sprintf("Session %s created for client %s", sessionID, clientID)
	log.Printf("Created %s for client %s", sessionID, clientID)
	return nil
}

func (s *Server) serverSession(sessionID string, requestCh chan common.Request, keepaliveCh chan common.Request, timeout time.Duration) {
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

// pass Servers []*Server
func (s *Server) processQueue(Servers []*Server) {
	for req := range s.requests {
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
					s.logString = fmt.Sprintf("%s, %s, %s", seatID, req.Type, req.ClientID)
					s.appendEntry(logString(s.logString), Servers)
					s.entryCommit()
					// s.saveSeats() // Save the updated seat map
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
					// s.appendEntry()
					s.logString = fmt.Sprintf("%s, %s, %s", seatID, req.Type, req.ClientID)
					s.appendEntry(logString(s.logString), Servers)
					s.entryCommit()
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
	}
}

func (s *Server) handleHeartbeats() {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	lastHeartbeat := time.Now()

	for {
		select {
		case <-ticker.C:
			// Check if the server is alive
			if !s.isAlive {
				continue
			}

			if s.role == Leader {
				// Leader sends heartbeats
				fmt.Printf("Server %d (Term %d) sending heartbeats\n", s.serverID, s.term)

				message := InternalMessage{
					SourceId: s.serverID,
					Type:     "HEARTBEAT",
					Data:     fmt.Sprintf("Heartbeat from Leader %d", s.serverID),
				}

				// Send heartbeat to other servers
				for i := range s.OutgoingCh {
					if i == s.serverID {
						continue
					}
					select {
					case s.OutgoingCh[i] <- message:
						// Message sent successfully
					default:
						fmt.Printf("Failed to send heartbeat to server %d (channel might be full)\n", i)
					}
				}
			} else {
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
					// resetting hearbeat
					lastHeartbeat = time.Now()
				}
			}

		case msg := <-s.IncomingCh:
			if !s.isAlive {
				continue
			}

			switch msg.Type {
			case "HEARTBEAT":
				fmt.Printf("Server %d received %s from Server %d\n", s.serverID, msg.Type, msg.SourceId)
				lastHeartbeat = time.Now()

				// If we receive a heartbeat and we're a candidate or follower, we might reset to follower if term is adequate
				// For simplicity, assume the leader always has equal or higher term.
				if s.role == Candidate {
					s.role = Follower
					s.votedFor = -1
					// Adjust term logic if needed
				}

			case "REQUESTVOTE":
				candidateID := msg.SourceId
				candidateTerm := msg.Data.(int)

				if candidateTerm > s.term {
					// Higher term: become follower and grant vote
					s.term = candidateTerm
					s.role = Follower
					s.votedFor = candidateID
					fmt.Printf("Server %d granted vote to Server %d for Term %d\n", s.serverID, candidateID, candidateTerm)

					// Send vote response with true
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
					// Same term, check if already voted
					if s.votedFor == -1 || s.votedFor == candidateID {
						// Grant vote
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
						// Deny vote because already voted for someone else
						fmt.Printf("Server %d denied vote to Server %d for Term %d (already voted for Server %d)\n", s.serverID, candidateID, candidateTerm, s.votedFor)

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
					// Deny vote because the candidate's term is stale
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

				// Handle vote requests here if desired
				// This is optional since we're only asked to implement sendRequestVoteRPC logic.
				// In a full implementation, you'd compare terms and possibly grant votes.
			case "VOTERESPONSE":
				// Handle incoming vote responses
				if s.role == Candidate {
					respData := msg.Data.(map[string]interface{})
					responseTerm := respData["term"].(int)
					voteGranted := respData["voteGranted"].(bool)

					// If the responder's term is higher, step down to follower
					if responseTerm > s.term {
						fmt.Printf("Server %d received VOTERESPONSE with higher term %d. Stepping down to Follower.\n", s.serverID, responseTerm)
						s.term = responseTerm
						s.role = Follower
						s.votedFor = -1
						return
					}

					// If the response term matches our current term
					if responseTerm == s.term {
						if voteGranted {
							s.votes++
							fmt.Printf("Server %d received vote. Total votes: %d\n", s.serverID, s.votes)

							// Check if we have the majority
							if s.votes > len(s.OutgoingCh)/2 {
								s.role = Leader
								s.isLeader = true
								s.votedFor = -1
								update_LoadBalancer(s.LocalPort, fmt.Sprintf("Server%d", s.serverID))

								fmt.Printf("Server %d became Leader for Term %d\n", s.serverID, s.term)
								// Start sending heartbeats immediately as leader
							}
						} else {
							// Vote not granted
							// fmt.Printf("Server %d received a negative vote response for Term %d.\n", s.serverID, responseTerm)
							// No specific action required for a negative vote unless you want to handle tie situations.
						}
					} else {
						// If the response is from an older term, ignore it
						fmt.Printf("Server %d received a stale VOTERESPONSE (term %d < current %d). Ignoring.\n", s.serverID, responseTerm, s.term)
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

// Log Replication //

// 1. Client Update (Read global queue [s.requests]) if write -> appendEntry (add entry to local log of each follower server)
// 1.1 appendEntry to leaderserver -> appendEntry to followers -> followers ConfirmAppend

// VERSION 1 APPEND ENTRY [NOT A CONTINUOUS PROCESS - Since we are not using heartbeat]
func (s *Server) appendEntry(logString logString, servers []*Server) {

	// TODO ACCESS MAXIMUM INDEX OF LOG SERVER
	// logIndexCounter := 0
	// logIndexCounter++

	leaderLogIndex := s.getMaxLogIndex(s) + 1

	// s.Log[logIndexCounter] = string(logString)
	logInfo := strings.Split(string(logString), ",")
	s.Log[leaderLogIndex] = string(logString)
	LseatID := logInfo[0]
	Loperation := logInfo[1]
	LclientID := logInfo[2]

	message := LogEntry{
		SeatID:    LseatID,
		Operation: Loperation,
		ClientID:  LclientID,
	}

	// Send to all other servers
	for i, server := range servers {
		fmt.Sprintf("How many server %d\n", i)
		// TODO
		// server.Log LOOK FOR MAXIMUM INDEX ?
		// follower maximum index < logIndexCounter
		maxFollowerIndex := server.getMaxLogIndex(server)

		// TODO COMPARE logIndexCounter WITH maxFollowerIndex
		if leaderLogIndex > maxFollowerIndex {
			// then only appendlogentrych
			fmt.Printf("This is the Log Entry message %s being sent from the leader server %d to follower %d\n",
				[]LogEntry{message}, s.serverID, server.serverID)
			server.AppendLogEntryCh <- []LogEntry{message}
			// ERROR : cannot use message (variable of type LogEntry) as []LogEntry value in sendcompilerIncompatibleAssign
			// TODO UPDATE THE FOLLOWER SERVER LOG
			if !server.isLeader {
				server.Log[maxFollowerIndex+1] = string(logString)
				fmt.Printf("This is the Server's full Log \n %+v \n", server.Log) // see entire Log map
			}
			s.ConfirmAppendCh <- ConfirmAppend{
				ConfirmAppend: "ConfirmAppend",
			}
		}
	}
	fmt.Printf("AppendEntry is completed Yay!\n")
}

// TODO CHECK ENTRY COMMIT WORKS
func (s *Server) entryCommit() {
	// count num of follower servers and num of confirmAppend received
	// if num of confirmAppend > num follower servers//2 -> update seatFile
	// check that more than half the num of AppendLogEntry is acknowledged thru ConfirmAppendCh
	if len(s.ConfirmAppendCh) > len(s.AppendLogEntryCh)/2 {
		s.saveSeats() // serve the client request and update the seatFile
		fmt.Printf("Successful entry commit & saved to seatFile\n")
	}
}

func (s *Server) getMaxLogIndex(server *Server) int {
	// For Log in 1 server
	// Iterate through the Log map
	// Get the latest & maximum index

	var maxLogIndex int
	for logIndex, _ := range server.Log {
		// if LogIndex
		maxLogIndex = logIndex
		break
	}

	for n := range server.Log {
		if n > maxLogIndex {
			maxLogIndex = n
			fmt.Printf("iter: maxFollowerIndex %d\n", maxLogIndex) // ITERATING THROUGH THE FOLLOWER LOG TO GET MAXIMUM
		}
	}
	fmt.Printf("maxLogIndex %d\n", maxLogIndex) // MAXIMUM INDEX OF FOLLOWER LOG

	return maxLogIndex
}

func main() {
	// Load the seat data from the file
	loadSeats(seatFile)

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

		// Start the server's request processing queue
		go server.processQueue(servers) // pass servers to processQueue

		// Start the server listener
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
		go server.handleHeartbeats()
	}

	//loadBalancer := init_LoadBalancer(servers)
	time.Sleep(5 * time.Second)

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
