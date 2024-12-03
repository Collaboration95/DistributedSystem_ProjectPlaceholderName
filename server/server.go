package main

import (
	"fmt"
	"log"
	"net"
	"net/rpc"
	"sync"

	"rpc-system/common"
)

// Server manages server sessions and processes requests
type Server struct {
	sessions   map[string]chan common.Request  // Map of session IDs to channels
	requests   chan common.Request             // Global processing queue
	responses  map[string]chan common.Response // Map of client request IDs to response channels
	seats      map[string]string               // Shared seat map
	mu         sync.Mutex                      // Protect shared resources
	sessionMux sync.Mutex                      // Protect session map
}

// NewServer initializes the server
func NewServer() *Server {
	return &Server{
		sessions:  make(map[string]chan common.Request),
		requests:  make(chan common.Request, 100), // Global processing queue
		responses: make(map[string]chan common.Response),
		seats: map[string]string{
			"1A": "available",
			"1B": "available",
			"2A": "available",
			"2B": "occupied",
		},
	}
}

// CreateSession creates a new server session
func (s *Server) CreateSession(clientID string, reply *string) error {
	s.sessionMux.Lock()
	defer s.sessionMux.Unlock()

	// Use clientID for sessionID
	sessionID := fmt.Sprintf("server-session-%s", clientID)

	// Create a channel for the session
	sessionCh := make(chan common.Request, 10) // Buffer size can be adjusted
	s.sessions[sessionID] = sessionCh

	// Start the server session
	go s.serverSession(sessionID, sessionCh)

	*reply = fmt.Sprintf("Session %s created for client %s", sessionID, clientID)
	log.Printf("Created %s for client %s", sessionID, clientID)
	return nil
}

// serverSession forwards requests to the global processing queue
func (s *Server) serverSession(sessionID string, requestCh chan common.Request) {
	log.Printf("Server session %s started", sessionID)

	for req := range requestCh {
		log.Printf("Session %s received request: %+v", sessionID, req)

		// Forward the request to the global processing queue
		s.requests <- req
	}

	log.Printf("Server session %s ended", sessionID)
}

// ProcessRequest handles client requests and queues them for asynchronous processing
func (s *Server) ProcessRequest(req *common.Request, res *common.Response) error {
	// Find the session channel
	s.sessionMux.Lock()
	sessionCh, sessionExists := s.sessions[req.ServerID]
	s.sessionMux.Unlock()

	if !sessionExists {
		res.Message = fmt.Sprintf("Session %s does not exist", req.ServerID)
		res.Status = "FAILURE"
		return nil
	}

	// Create a response channel for this request
	responseCh := make(chan common.Response, 1) // Buffer size of 1
	s.mu.Lock()
	s.responses[req.ClientID] = responseCh
	s.mu.Unlock()

	// Forward the request to the session channel
	sessionCh <- *req

	// Wait for the response
	response := <-responseCh
	res.Status = response.Status
	res.Message = response.Message

	// Clean up the response channel
	s.mu.Lock()
	delete(s.responses, req.ClientID)
	s.mu.Unlock()

	return nil
}

// processQueue processes requests from the global processing queue
func (s *Server) processQueue() {
	for req := range s.requests {
		s.mu.Lock()

		responseMessage := ""
		status := "SUCCESS"

		switch req.Type {
		case "RESERVE":
			if seatStatus, exists := s.seats[req.SeatID]; exists {
				if seatStatus == "available" {
					s.seats[req.SeatID] = "occupied"
					responseMessage = fmt.Sprintf("Seat %s reserved for client %s by %s", req.SeatID, req.ClientID, req.ServerID)
				} else {
					responseMessage = fmt.Sprintf("Seat %s already occupied. Client %s by %s", req.SeatID, req.ClientID, req.ServerID)
					status = "FAILURE"
				}
			} else {
				responseMessage = fmt.Sprintf("Seat %s does not exist. Client %s by %s", req.SeatID, req.ClientID, req.ServerID)
				status = "FAILURE"
			}
		default:
			responseMessage = fmt.Sprintf("Invalid operation %s by client %s via %s", req.Type, req.ClientID, req.ServerID)
			status = "FAILURE"
		}

		log.Printf("[%s] %s", status, responseMessage)

		// Send the response back to the client
		responseCh := s.responses[req.ClientID]
		responseCh <- common.Response{
			Status:  status,
			Message: responseMessage,
		}

		s.mu.Unlock()
	}
}

func main() {
	server := NewServer()
	err := rpc.Register(server)
	if err != nil {
		log.Fatalf("Error registering server: %s", err)
	}

	// Start the global processing queue goroutine
	go server.processQueue()

	listener, err := net.Listen("tcp", ":12345") // Server listens on port 12345
	if err != nil {
		log.Fatalf("Error starting server: %s", err)
	}
	defer listener.Close()

	log.Println("Server is running on port 12345")
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("Connection error:", err)
			continue
		}
		go rpc.ServeConn(conn)
	}
}
