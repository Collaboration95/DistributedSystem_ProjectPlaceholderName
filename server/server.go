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
)

const (
	seatFile   = "seats.txt"
	numServers = 5
	timeout    = 100 * time.Second
	interval   = 5 * time.Second
)

type InternalMessage struct {
	SourceId int
	Type     string
	Data     interface{}
}

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

type Server struct {
	LocalPort  string
	serverID   int
	isLeader   bool
	OutgoingCh []chan InternalMessage
	IncomingCh chan InternalMessage
	sessions   map[string]struct {
		requestCh   chan common.Request
		keepaliveCh chan common.Request
	} // Map of session IDs to channels
	requests     chan common.Request // Global processing queue
	responses    map[string]chan common.Response
	seats        map[string]Seat
	mu           sync.Mutex
	sessionMux   sync.Mutex
	keepaliveMux sync.Mutex
	filePath     string // File path to seat map
}

func init_Server(numServer int, filePath string) []*Server {
	servers := make([]*Server, numServer)
	for i := 0; i < numServer; i++ {
		servers[i] = &Server{
			LocalPort: fmt.Sprintf(":%d", 12346+i),
			serverID:  i,
			isLeader:  i == 0, // first server i sleader
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

	lastHeartbeat := time.Now()
	timer := time.NewTimer(timeout)

	for {
		select {
		case req := <-keepaliveCh:
			if req.Type == "KEEPALIVE" {
				lastHeartbeat = time.Now()
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
			if time.Since(lastHeartbeat) > timeout {
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

func (s *Server) processQueue() {
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
			// Ensure only the current leader sends heartbeats
			if s.isLeader {
				fmt.Printf("Server %d sending heartbeats \n", s.serverID)

				message := InternalMessage{
					SourceId: s.serverID,
					Type:     "HEARTBEAT",
					Data:     fmt.Sprintf("Heartbeat from Leader %d", s.serverID),
				}

				// Improve heartbeat sending logic
				for i := range s.OutgoingCh {
					if i == s.serverID {
						continue
					}
					select {
					case s.OutgoingCh[i] <- message:
						// Message sent successfully
					default:
						// Log or handle channel blocking
						fmt.Printf("Failed to send heartbeat to server %d (channel might be full)\n", i)
					}
				}
			} else {
				// Non-leader timeout check
				if time.Since(lastHeartbeat) > timeout {
					log.Printf("Server %d: No heartbeat received within timeout period! Potential leader failure.", s.serverID)
					// Optional: Trigger leader election or recovery mechanism
				}
			}

		case msg := <-s.IncomingCh:
			// Only process and update lastHeartbeat for HEARTBEAT messages
			if msg.Type == "HEARTBEAT" {
				fmt.Printf("Server %d received %s from Server %d\n", s.serverID, msg.Type, msg.SourceId)
				lastHeartbeat = time.Now() // Update last heartbeat time
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
		go server.processQueue()

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

	loadBalancer := init_LoadBalancer(servers)

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
