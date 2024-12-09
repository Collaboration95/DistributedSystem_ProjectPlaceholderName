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
	seatFile = "seats.txt"
)

type LoadBalancer struct {
	LeaderPort string
}

type Seat struct {
	SeatID   string
	Status   string
	ClientID string
}

func getClientIDfromSessionID(sessionID string) string {
	return strings.Split(sessionID, "-")[0]
}

type Server struct {
	LocalPort string
	serverID  int
	sessions  map[string]struct {
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
			sessions: make(map[string]struct {
				requestCh   chan common.Request
				keepaliveCh chan common.Request
			}),
			requests:  make(chan common.Request, 100), // Global processing queue
			responses: make(map[string]chan common.Response),
			seats:     make(map[string]Seat),
			filePath:  filePath,
		}
		err := servers[i].loadSeats()
		if err != nil {
			log.Fatalf("Failed to load seats from file: %s", err)
		}

	}

	return servers
}

// func NewServer(filePath string) *Server {
// 	server := &Server{
// 		sessions: make(map[string]struct {
// 			requestCh   chan common.Request
// 			keepaliveCh chan common.Request
// 		}),
// 		requests:  make(chan common.Request, 100), // Global processing queue
// 		responses: make(map[string]chan common.Response),
// 		seats:     make(map[string]Seat),
// 		filePath:  filePath,
// 	}
// 	err := server.loadSeats()
// 	if err != nil {
// 		log.Fatalf("Failed to load seats from file: %s", err)
// 	}
// 	return server
// }

// loadSeats reads the seat data from a file and categorizes them into available and unavailable seats
func (s *Server) loadSeats() error {
	file, err := os.Open(s.filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	var availableSeats []string
	var unavailableSeats []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		parts := strings.Split(line, ":")
		if len(parts) == 3 {
			seatID := strings.ToUpper(strings.TrimSpace(parts[0]))
			status := strings.TrimSpace(parts[1])
			clientID := strings.TrimSpace(parts[2])
			s.seats[seatID] = Seat{
				SeatID:   seatID,
				Status:   status,
				ClientID: clientID,
			}

			if status == "available" {
				availableSeats = append(availableSeats, seatID)
			} else {
				unavailableSeats = append(unavailableSeats, seatID)
			}
		}
	}
	log.Printf("Available seats: %v", availableSeats)
	log.Printf("Unavailable seats: %v", unavailableSeats)
	return scanner.Err()
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
					if getClientIDfromSessionID(req.ClientID) != getClientIDfromSessionID(seat.ClientID) {
						status = "FAILURE"
						responseMessage = fmt.Sprintf("Seat %s is occupied by another client %s. Cannot cancel. Client %s by %s", seatID, seat.ClientID, req.ClientID, req.ServerID)
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

func (lb *LoadBalancer) GetLeaderIP(req *common.Request, res *common.Response) error {
	fmt.Println("Load balancer received request for leader IP sending back leader port")
	res.Status = "SUCCESS"
	res.Message = lb.LeaderPort
	return nil
}

func main() {
	const seatFile = "seats.txt"

	loadBalancer := &LoadBalancer{
		LeaderPort: ":12346",
	}

	errLoadBalancer := rpc.Register(loadBalancer)
	if errLoadBalancer != nil {
		log.Fatalf("Error registering load balancer: %s", errLoadBalancer)
	}

	lbListener, errlb := net.Listen("tcp", ":12345")
	if errlb != nil {
		log.Fatalf("Error starting load balancer listener: %s", errlb)
	}
	defer lbListener.Close()
	log.Println("LoadBalancer is running on port 12345")

	servers := init_Server(1, seatFile)
	server := servers[0]
	err := rpc.Register(server)
	if err != nil {
		log.Fatalf("Error registering server: %s", err)
	}

	go server.processQueue()

	listener, err := net.Listen("tcp", server.LocalPort)
	if err != nil {
		log.Fatalf("Error starting server listener: %s", err)
	}
	defer listener.Close()
	log.Printf("Server is running on port %s\n", server.LocalPort)

	// Start a goroutine to handle load balancer requests
	go func() {
		for {
			conn, err := lbListener.Accept()
			if err != nil {
				log.Println("Load Balancer Connection error:", err)
				continue
			}
			go rpc.ServeConn(conn)
		}
	}()

	// Main loop to handle server requests
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("Server Connection error:", err)
			continue
		}
		go rpc.ServeConn(conn)
	}
}
