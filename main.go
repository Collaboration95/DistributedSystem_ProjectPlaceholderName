package main

import (
	"fmt"
	"log"
	"time"
)

// Helper function to check errors
func checkError(err error) {
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func main() {
	// Simulate client interactions
	leaderAddress := "localhost:8000" // Leader node address
	clientID := 1                     // Simulated client ID
	clientID2 := 2                    // Simulated second client ID
	clientID3 := 3                    // Simulated third client ID

	// Initialize a session for the first client
	session := &Session{}
	err := session.InitSession(clientID, []string{leaderAddress})
	checkError(err)

	// Test locking a seat
	fmt.Println("Attempting to reserve seat-1 by Client 1...")
	err = session.RequestLock("seat-1")
	checkError(err)
	fmt.Println("Seat-1 reserved successfully by Client 1.")

	// Initialize a session for the second client
	session2 := &Session{}
	err = session2.InitSession(clientID2, []string{leaderAddress})
	checkError(err)

	// Test that Client 2 cannot request a lock on a reserved seat (seat-1)
	fmt.Println("Attempting to reserve (an already reserved) seat-1 by Client 2...")
	err = session2.RequestLock("seat-1")
	if err != nil {
		fmt.Printf("Client 2 failed to reserve seat-1: %v\n", err)
	} else {
		fmt.Println("Client 2 reserved seat-1 successfully (unexpected).")
	}

	// Test booking the reserved seat
	fmt.Println("Client 1 attempting to book seat-1...")
	err = session.BookSeat("seat-1")
	checkError(err)
	fmt.Println("Seat-1 booked successfully by Client 1.")

	// Test that Client 2 cannot request a lock for a booked seat (seat-1)
	fmt.Println("Attempting to reserve (an already booked) seat-1 by Client 2...")
	err = session2.RequestLock("seat-1")
	if err != nil {
		fmt.Printf("Client 2 failed to reserve seat-1 (expected): %v\n", err)
	} else {
		fmt.Println("Client 2 reserved seat-1 successfully (unexpected).")
	}

	// Test locking another seat by Client 2
	fmt.Println("Client 2 attempting to reserve seat-2...")
	err = session2.RequestLock("seat-2")
	checkError(err)
	fmt.Println("Seat-2 reserved successfully by Client 2.")

	// Test releasing a lock on seat-2
	fmt.Println("Client 2 releasing seat-2 lock...")
	err = session2.ReleaseLock("seat-2")
	checkError(err)
	fmt.Println("Seat-2 lock released by Client 2.")

	// Initialize a session for the third client
	session3 := &Session{}
	err = session3.InitSession(clientID3, []string{leaderAddress})
	checkError(err)

	// Test locking seat by Client 3
	fmt.Println("Client 3 attempting to reserve seat-40...")
	err = session3.RequestLock("seat-40")
	checkError(err)
	fmt.Println("Seat-40 reserved successfully by Client 3.")

	// Test booking the reserved seat
	fmt.Println("Client 3 attempting to book seat-40...")
	err = session3.BookSeat("seat-40")
	checkError(err)
	fmt.Println("Seat-40 booked successfully by Client 3.")

	// Test releasing a lock on seat-2
	fmt.Println("Client 3 releasing seat-40 lock...")
	err = session3.ReleaseLock("seat-40")
	if err != nil {
		fmt.Printf("Client 3 failed to release seat-40 (expected): %v\n", err)
	} else {
		fmt.Println("Client 3 released lock on seat-40 successfully (unexpected).")
	}

	// Test keep-alive functionality
	fmt.Println("Starting keep-alive for Client 1...")
	go func() {
		for {
			// Keep-alive every 30 seconds
			time.Sleep(30 * time.Second)
			// Call the keep-alive function
			args := KeepAliveArgs{ClientID: fmt.Sprint(clientID)}
			var reply KeepAliveResponse
			err := session.rpcClient.Call("ChubbyNode.KeepAlive", args, &reply)
			if err == nil && reply.Success {
				fmt.Println("Keep-alive sent for Client 1.")
			} else {
				fmt.Printf("Failed to send keep-alive for Client 1: %v\n", err)
			}
		}
	}()

	// Allow some time for keep-alive messages to be sent
	time.Sleep(90 * time.Second)

	// Close the sessions after use
	session.CloseSession()
	session2.CloseSession()
	fmt.Println("Sessions closed.")
}

/* What is missing / additional things to add
1. MonitorSession is removed
2. There is no leader election / simulate leader failure
3. Need a better way to test keep-alive
4. More....
*/
