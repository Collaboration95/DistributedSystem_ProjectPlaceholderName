package main

import (
	"fmt"
	"log"
	"net"
	"net/rpc"
	"sync"
	"time"

	"rpc-system/common"
)

type Server struct {
	sessions map[string]struct {
		requestCh   chan common.Request
		keepaliveCh chan common.Request
	} // Map of session IDs to channels
	requests     chan common.Request             // Global processing queue
	responses    map[string]chan common.Response // Map of unique request IDs to response channels
	seats        map[string]string               // Shared seat map
	mu           sync.Mutex                      // Protect shared resources
	sessionMux   sync.Mutex                      // Protect session map
	keepaliveMux sync.Mutex                      // Protect keepalive resources
}

func NewServer() *Server {
	return &Server{
		sessions: make(map[string]struct {
			requestCh   chan common.Request
			keepaliveCh chan common.Request
		}),
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
				log.Printf("Session %s received KeepAlive from client %s", sessionID, req.ClientID)

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
		switch req.Type {
		case "RESERVE":
			if seatStatus, exists := s.seats[req.SeatID]; exists {
				if seatStatus == "available" {
					s.seats[req.SeatID] = "occupied"
					responseMessage = fmt.Sprintf("Seat %s reserved for client %s by %s", req.SeatID, req.ClientID, req.ServerID)
				} else {
					status = "FAILURE"
					responseMessage = fmt.Sprintf("Seat %s already occupied. Client %s by %s", req.SeatID, req.ClientID, req.ServerID)
				}
			} else {
				status = "FAILURE"
				responseMessage = fmt.Sprintf("Seat %s does not exist. Client %s by %s", req.SeatID, req.ClientID, req.ServerID)
			}
		default:
			status = "FAILURE"
			responseMessage = fmt.Sprintf("Invalid operation %s by client %s via %s", req.Type, req.ClientID, req.ServerID)
		}
		s.mu.Unlock()

		log.Printf("[%s] %s", status, responseMessage)

		if responseCh, exists := s.responses[req.ClientID]; exists {
			responseCh <- common.Response{Status: status, Message: responseMessage}
		}
	}
}

func main() {
	server := NewServer()
	err := rpc.Register(server)
	if err != nil {
		log.Fatalf("Error registering server: %s", err)
	}

	go server.processQueue()

	listener, err := net.Listen("tcp", ":12345")
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
