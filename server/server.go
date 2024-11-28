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
	sessions   map[string]chan common.Request // Map of session IDs to channels
	requests   chan common.Request            // Shared request queue
	seats      map[string]string              // Shared seat map
	mu         sync.Mutex                     // Protect shared resources
	sessionMux sync.Mutex                     // Protect session map
	nextID     int                            // Next session ID (simple counter)
}

// NewServer initializes the server
func NewServer() *Server {
	return &Server{
		sessions: make(map[string]chan common.Request),
		requests: make(chan common.Request, 100),
		seats: map[string]string{
			"1A": "available",
			"1B": "available",
			"2A": "available",
			"2B": "occupied",
		},
		nextID: 1,
	}
}

// CreateSession creates a new server session
func (s *Server) CreateSession(clientID string, reply *string) error {
	s.sessionMux.Lock()
	defer s.sessionMux.Unlock()

	// Assign a unique session ID
	sessionID := fmt.Sprintf("server-session-%d", s.nextID)
	s.nextID++

	sessionCh := make(chan common.Request)
	s.sessions[sessionID] = sessionCh

	// Start the server session
	go s.serverSession(sessionID, sessionCh)

	*reply = fmt.Sprintf("Session %s created for client %s", sessionID, clientID)
	log.Printf("Created %s for client %s", sessionID, clientID)
	return nil
}

// serverSession handles requests for a specific client
func (s *Server) serverSession(sessionID string, requestCh chan common.Request) {
	log.Printf("Server session %s started", sessionID)
	for req := range requestCh {
		req.ServerID = sessionID
		s.requests <- req // Forward request to shared queue
	}
}

// ProcessRequest handles client requests
func (s *Server) ProcessRequest(req *common.Request, res *common.Response) error {
	s.sessionMux.Lock()
	sessionCh, exists := s.sessions[req.ServerID]
	s.sessionMux.Unlock()

	if !exists {
		res.Message = fmt.Sprintf("Session %s does not exist", req.ServerID)
		return nil
	}

	// Forward the request to the session
	sessionCh <- *req
	res.Message = fmt.Sprintf("Request forwarded to session %s", req.ServerID)
	return nil
}

// processQueue processes requests from the shared queue
func (s *Server) processQueue() {
	for req := range s.requests {
		s.mu.Lock()
		switch req.Type {
		case "RESERVE":
			if status, exists := s.seats[req.SeatID]; exists {
				if status == "available" {
					s.seats[req.SeatID] = "occupied"
					log.Printf("Seat %s reserved for client %s by %s", req.SeatID, req.ClientID, req.ServerID)
				} else {
					log.Printf("Seat %s already occupied. Client %s by %s", req.SeatID, req.ClientID, req.ServerID)
				}
			} else {
				log.Printf("Seat %s does not exist. Client %s by %s", req.SeatID, req.ClientID, req.ServerID)
			}
		default:
			log.Printf("Invalid operation %s by client %s via %s", req.Type, req.ClientID, req.ServerID)
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

	go server.processQueue() // Start processing the shared queue

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
