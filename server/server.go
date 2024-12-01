package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/rpc"
	"os"
	"strings"
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
	filePath   string                         // File path to seat map
}

// NewServer initializes the server
func NewServer(filePath string) *Server {
	server := &Server{
		sessions: make(map[string]chan common.Request),
		requests: make(chan common.Request, 100),
		seats:    make(map[string]string),
		nextID:   1,
		filePath: filePath,
	}
	err := server.loadSeats()
	if err != nil {
		log.Fatalf("Failed to load seats from file: %s", err)
	}
	return server
}

// loadSeats reads the seat data from a file
func (s *Server) loadSeats() error {
	file, err := os.Open(s.filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ":")
		if len(parts) == 2 {
			seatID := strings.TrimSpace(parts[0])
			status := strings.TrimSpace(parts[1])
			s.seats[seatID] = status
		}
	}
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
	for seatID, status := range s.seats {
		_, err := fmt.Fprintf(writer, "%s: %s\n", seatID, status)
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
					err := s.saveSeats()
					if err != nil {
						log.Printf("Error saving seats to file: %s", err)
					}
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
	const seatFile = "seats.txt"
	server := NewServer(seatFile)
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