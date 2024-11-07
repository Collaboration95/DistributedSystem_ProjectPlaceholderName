package main

import (
	"fmt"
	"sync"

	"github.com/Collaboration95/DistributedSystem_ProjectPlaceholderName.git/api"
	"github.com/Collaboration95/DistributedSystem_ProjectPlaceholderName.git/client"
	"github.com/Collaboration95/DistributedSystem_ProjectPlaceholderName.git/server"
)

// func main() {
// 	// Create the server and start listening for client connections
// 	serverAddr := "localhost:1234"
// 	server := server.NewServer() // You can instantiate your server from the `server` package
// 	err := server.Start(serverAddr)
// 	if err != nil {
// 		log.Fatalf("Failed to start the server: %v", err)
// 	}

// 	// Start the server in a separate goroutine to simulate multiple clients
// 	go func() {
// 		// Simulate multiple client connections
// 		for i := 1; i <= 5; i++ {
// 			clientID := api.ClientID(fmt.Sprintf("client-%d", i))
// 			clientSess, err := client.InitSession(clientID)
// 			if err != nil {
// 				log.Printf("Failed to initialize session for client-%d: %v", i, err)
// 				continue
// 			}

// 			// Simulate client operations (open locks, acquire locks, etc.)
// 			go func(sess *client.ClientSession) {
// 				// Client operations (such as acquiring locks, etc.)
// 				// For example:
// 				_, err := sess.TryAcquireLock("file1.txt", api.EXCLUSIVE)
// 				if err != nil {
// 					log.Printf("Client %s failed to acquire lock: %v", clientID, err)
// 				}
// 			}(clientSess)
// 		}
// 	}()

// 	// Keep the server running
// 	select {}
// }

// func main() {
// 	serverAddr := "localhost:12345"

// 	// Start the server in a separate goroutine
// 	go func() {
// 		server.StartServer(serverAddr)
// 	}()

// 	// Simulate multiple clients trying to connect to the server
// 	var wg sync.WaitGroup
// 	for i := 1; i <= 5; i++ {
// 		wg.Add(1)
// 		go func(clientID string) {
// 			defer wg.Done()
// 			client.StartClient(clientID, serverAddr)
// 		}(i)
// 	}

//		wg.Wait()
//		fmt.Println("All clients have finished their requests.")
//	}
func main() {
	// serverAddr := "localhost:12345"
	var wg sync.WaitGroup

	// // Start the server in a separate goroutine
	// go func() {
	// 	server.StartServer(serverAddr)
	// }()
	// Start 5 servers with different IDs
	serverManager := &server.ServerManager{}
	serverManager.StartServers()

	// Simulate multiple clients trying to connect to the server  with the highest ID
	// var wg sync.WaitGroup
	for i := 1; i <= 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			// Find the highest server address (server with ID 5)
			// Convert the integer to string and use it as a clientID
			// clientID := api.ClientID(strconv.Itoa(i)) // Assuming api.ClientID is a type alias for string
			clientID := api.ClientID(fmt.Sprintf("client-%d", i))
			serverAddr := fmt.Sprintf("localhost:%d", 12345+5)
			// Start the client with the clientID and server address
			client.StartClient(clientID, serverAddr)
		}(i)
	}

	wg.Wait()
	fmt.Println("All clients have finished their requests.")
}
