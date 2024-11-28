package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/rpc"
	"os"
	"strings"
)

type ServerSession struct {
	seats map[string]string
}

// Initialize the seat map by reading from a predefined seats.txt file
func (s *ServerSession) initializeSeatMap() error {
	s.seats = make(map[string]string)

	file, err := os.Open("seats.txt")
	if err != nil {
		return fmt.Errorf("could not open seats.txt: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ":")
		if len(parts) == 2 {
			seatID := parts[0]
			status := parts[1]
			s.seats[seatID] = status
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading seats.txt: %w", err)
	}

	return nil
}

// Ping is a simple method to keep the connection alive
func (s *ServerSession) Ping(args PingArgs, reply *PingReply) error {
	// Send a "pong" message for keepalive
	reply.Message = "pong"
	return nil
}

// RequestLock handles seat reservation requests
func (s *ServerSession) RequestLock(args *RequestArgs, reply *ServerResponse) error {
	// Initialize seat map if not already initialized
	if s.seats == nil {
		err := s.initializeSeatMap()
		if err != nil {
			return err
		}
	}

	// Handle seat reservation logic
	if args.Type == "RESERVE" {
		if status, exists := s.seats[args.SeatID]; exists {
			if status == "available" {
				s.seats[args.SeatID] = "occupied"
				reply.Message = fmt.Sprintf("Seat %s reserved for client %s", args.SeatID, args.ClientID)
			} else {
				reply.Message = fmt.Sprintf("Seat %s is already occupied.", args.SeatID)
			}
		} else {
			reply.Message = fmt.Sprintf("Seat %s does not exist.", args.SeatID)
		}
	} else {
		reply.Message = fmt.Sprintf("Invalid operation: %s", args.Type)
	}

	return nil
}

// Server_Session handles server connection and requests
func Server_Session(port string) {
	server := new(ServerSession)
	err := rpc.Register(server)
	if err != nil {
		log.Fatal("Error registering server:", err)
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%s", port))
	if err != nil {
		log.Fatal("Error starting server on port", port, ":", err)
	}
	defer listener.Close()

	log.Printf("Server started on port %s\n", port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("Connection error:", err)
			continue
		}
		go rpc.ServeConn(conn)
	}
}
