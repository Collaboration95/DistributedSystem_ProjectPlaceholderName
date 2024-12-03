//client/client.go

package main

import (
	"flag"
	"fmt"
	"log"
	"net/rpc"
	"rpc-system/common"
	"sync"
	"time"
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

func connectToMasterServer() (*rpc.Client, error) {
	client, err := rpc.Dial("tcp", "127.0.0.1:12345") // Connect to server
	if err != nil {
		return nil, err
	}
	return client, nil
}
func startClientSession(clientID string, rpcClient *rpc.Client) {
	var reply string
	err := rpcClient.Call("Server.CreateSession", clientID, &reply)
	if err != nil {
		log.Fatalf("[Client %s] Error creating session: %s", clientID, err)
	}
	log.Printf("[Client %s] Session created: %s", clientID, reply)

	serverID := fmt.Sprintf("server-session-%s", clientID)
	var wg sync.WaitGroup
	wg.Add(1)

	go clientSession(clientID, serverID, []common.Request{
		{SeatID: "1A", Type: "RESERVE"},
		{SeatID: "1B", Type: "RESERVE"},
	}, rpcClient, &wg)

	wg.Wait()
}

func getClientID() string {
	clientID := flag.String("clientID", "", "Unique client ID")
	flag.Parse()
	if *clientID == "" {
		log.Fatalf("Client ID is required. Use --clientID flag to specify one.")
	}
	return *clientID
}

func main() {
	clientID := getClientID()

	//currently assuming one server first
	//client, err := rpc.Dial("tcp", "127.0.0.1:12345") // Connect to server
	//server team to rework this part after implementing replicas
	client, err := connectToMasterServer()
	if err != nil {
		log.Fatalf("Error connecting to server: %s", err)
	}
	defer client.Close()
	startClientSession(clientID, client)
}
