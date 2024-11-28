package main

import (
	"fmt"
	"log"
	"net/rpc"
	"sync"
	"time"

	"rpc-system/common"
)

func clientSession(clientID, serverID string, requests []common.Request, client *rpc.Client, wg *sync.WaitGroup) {
	defer wg.Done()

	for _, req := range requests {
		req.ClientID = clientID
		req.ServerID = serverID

		var res common.Response
		err := client.Call("Server.ProcessRequest", &req, &res)
		if err != nil {
			log.Printf("[Client %s] Error sending request: %s", clientID, err)
			continue
		}
		log.Printf("[Client %s] %s", clientID, res.Message)
		time.Sleep(500 * time.Millisecond) // Simulate delay
	}
}

func main() {
	client, err := rpc.Dial("tcp", "127.0.0.1:12345") // Connect to server
	if err != nil {
		log.Fatalf("Error connecting to server: %s", err)
	}
	defer client.Close()

	// Client 1
	clientID1 := "client-1"
	var reply1 string
	err = client.Call("Server.CreateSession", clientID1, &reply1)
	if err != nil {
		log.Fatalf("[Client %s] Error creating session: %s", clientID1, err)
	}
	log.Printf("[Client %s] %s", clientID1, reply1)

	serverID1 := fmt.Sprintf("server-session-1")

	// Client 2
	clientID2 := "client-2"
	var reply2 string
	err = client.Call("Server.CreateSession", clientID2, &reply2)
	if err != nil {
		log.Fatalf("[Client %s] Error creating session: %s", clientID2, err)
	}
	log.Printf("[Client %s] %s", clientID2, reply2)

	serverID2 := fmt.Sprintf("server-session-2")

	// Client 3
	clientID3 := "client-3"
	var reply3 string
	err = client.Call("Server.CreateSession", clientID3, &reply3)
	if err != nil {
		log.Fatalf("[Client %s] Error creating session: %s", clientID3, err)
	}
	log.Printf("[Client %s] %s", clientID3, reply3)

	serverID3 := fmt.Sprintf("server-session-3")

	// Start client sessions
	var wg sync.WaitGroup
	wg.Add(2)

	go clientSession(clientID1, serverID1, []common.Request{
		{SeatID: "1A", Type: "RESERVE"},
		{SeatID: "1B", Type: "RESERVE"},
		{SeatID: "2A", Type: "RESERVE"},
		{SeatID: "2B", Type: "RESERVE"},
	}, client, &wg)

	go clientSession(clientID2, serverID2, []common.Request{
		{SeatID: "2A", Type: "RESERVE"},
		{SeatID: "2B", Type: "RESERVE"},
		{SeatID: "1A", Type: "RESERVE"},
		{SeatID: "1B", Type: "RESERVE"},
	}, client, &wg)
	go clientSession(clientID3, serverID3, []common.Request{
		{SeatID: "2A", Type: "RESERVE"},
		{SeatID: "2B", Type: "RESERVE"},
		{SeatID: "1A", Type: "RESERVE"},
		{SeatID: "1B", Type: "RESERVE"},
	}, client, &wg)
	wg.Wait()
}
