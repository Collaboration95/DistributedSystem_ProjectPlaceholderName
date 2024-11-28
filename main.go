package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// Request represents a client request
type Request struct {
	ClientID string
	SeatID   string
	Type     string
	ReplyCh  chan Response
}

// Response represents the server's response
type Response struct {
	Message string
	Err     error
}

// Shared queue for all server sessions
var requestQueue = make(chan Request, 100) // Buffered channel for requests

// Mutex for atomic file updates
var fileLock sync.Mutex

// Shared seat map
var seats = make(map[string]string)

// Initialize the seat map from the seats.txt file
func initializeSeatMap() {
	file, err := os.Open("seats.txt")
	if err != nil {
		log.Fatalf("Could not open seats.txt: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ":")
		if len(parts) == 2 {
			seats[parts[0]] = parts[1]
		}
	}
	if err := scanner.Err(); err != nil {
		log.Fatalf("Error reading seats.txt: %v", err)
	}
}

// Update the seats.txt file with the current seat map
func updateSeatFile() {
	fileLock.Lock()
	defer fileLock.Unlock()

	file, err := os.Create("seats.txt")
	if err != nil {
		log.Fatalf("Error updating seats.txt: %v", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for seatID, status := range seats {
		line := fmt.Sprintf("%s:%s\n", seatID, status)
		_, err := writer.WriteString(line)
		if err != nil {
			log.Fatalf("Error writing to seats.txt: %v", err)
		}
	}
	writer.Flush()
}

// Process requests from the shared queue
func processRequests() {
	for req := range requestQueue {
		if req.Type == "RESERVE" {
			if status, exists := seats[req.SeatID]; exists {
				if status == "available" {
					seats[req.SeatID] = "occupied"
					req.ReplyCh <- Response{Message: fmt.Sprintf("Seat %s reserved for client %s", req.SeatID, req.ClientID)}
				} else {
					req.ReplyCh <- Response{Message: fmt.Sprintf("Seat %s is already occupied.", req.SeatID)}
				}
			} else {
				req.ReplyCh <- Response{Message: fmt.Sprintf("Seat %s does not exist.", req.SeatID)}
			}
		} else {
			req.ReplyCh <- Response{Message: "Invalid operation"}
		}

		// Update the file after processing each request
		updateSeatFile()
	}
}

// Server session handling requests from its clients
func serverSession(serverID string, requestCh chan Request) {
	for req := range requestCh {
		req.ReplyCh = make(chan Response) // Create a reply channel for each request
		requestQueue <- req               // Push the request to the shared queue
		response := <-req.ReplyCh         // Wait for the response
		fmt.Printf("Server %s: %s\n", serverID, response.Message)
	}
}

// Client session sending requests concurrently to its server
func clientSession(clientID string, serverCh chan Request, requests []Request) {
	var wg sync.WaitGroup

	for _, req := range requests {
		wg.Add(1)
		go func(r Request) {
			defer wg.Done()
			r.ClientID = clientID
			serverCh <- r

			// Simulate random delays between requests
			time.Sleep(time.Duration(500+time.Now().UnixNano()%500) * time.Millisecond)
		}(req)
	}

	// Wait for all goroutines to finish
	wg.Wait()
}

func main() {
	// Initialize the seat map from the file
	initializeSeatMap()

	// Start the global request processor
	go processRequests()

	// Create channels for server sessions
	serverCh1 := make(chan Request)
	serverCh2 := make(chan Request)

	// Start server sessions
	go serverSession("server1", serverCh1)
	go serverSession("server2", serverCh2)

	// Start client sessions with concurrent requests
	go clientSession("client1", serverCh1, []Request{
		{SeatID: "1A", Type: "RESERVE"},
		{SeatID: "2A", Type: "RESERVE"},
		{SeatID: "3A", Type: "RESERVE"},
		{SeatID: "4A", Type: "RESERVE"},
		{SeatID: "1B", Type: "RESERVE"},
		{SeatID: "2B", Type: "RESERVE"},
		{SeatID: "3B", Type: "RESERVE"},
		{SeatID: "4B", Type: "RESERVE"},
	})

	go clientSession("client2", serverCh2, []Request{
		{SeatID: "1A", Type: "RESERVE"},
		{SeatID: "2A", Type: "RESERVE"},
		{SeatID: "3A", Type: "RESERVE"},
		{SeatID: "4A", Type: "RESERVE"},
		{SeatID: "1B", Type: "RESERVE"},
		{SeatID: "2B", Type: "RESERVE"},
		{SeatID: "3B", Type: "RESERVE"},
		{SeatID: "4B", Type: "RESERVE"},
	})

	// Keep the main function running
	select {}
}
