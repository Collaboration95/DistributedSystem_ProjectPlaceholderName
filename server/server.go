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
	"time"

	"rpc-system/common"
)

type Seat struct {
	Status    string
	ClientID  string
}

type Server struct {
	sessions map[string]struct {
		requestCh   chan common.Request
		keepaliveCh chan common.Request
	} // Map of session IDs to channels
	requests     chan common.Request             // Global processing queue
	responses    map[string]chan common.Response // Map of unique request IDs to response channels
	seats        map[string]Seat               // Shared seat map
	mu           sync.Mutex                      // Protect shared resources
	sessionMux   sync.Mutex                      // Protect session map
	keepaliveMux sync.Mutex                      // Protect keepalive resources
	filePath   string                         // File path to seat map
}

func NewServer(filePath string) *Server {
	server:= &Server{
		sessions: make(map[string]struct {
			requestCh   chan common.Request
			keepaliveCh chan common.Request
		}),
		requests:  make(chan common.Request, 100), // Global processing queue
		responses: make(map[string]chan common.Response),
		seats: make(map[string]Seat),
		filePath:  filePath,
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
        line := strings.TrimSpace(scanner.Text())
        parts := strings.Split(line, ":")
        if len(parts) == 3 {
            seatID := strings.ToUpper(strings.TrimSpace(parts[0])) 
            status := strings.TrimSpace(parts[1])
			clientID := strings.TrimSpace(parts[2])
            s.seats[seatID] = Seat{ 
				Status:   status,
				ClientID: clientID,
			}
            log.Printf("Loaded seat: %s - %s", seatID, status)
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
	for seatID, seat := range s.seats {
		_, err := fmt.Fprintf(writer, "%s: %s, %s\n", seatID, seat.Status, seat.ClientID)
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
        seatID := strings.ToUpper(strings.TrimSpace(req.SeatID)) // Normalize SeatID
        if seat, exists := s.seats[seatID]; exists {
            switch req.Type {
            case "RESERVE":
                if seat.Status == "available" {
                    s.seats[seatID] = Seat{
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
                    s.seats[seatID] = Seat{
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

func main() {
	const seatFile = "seats.txt"
	server := NewServer(seatFile)
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