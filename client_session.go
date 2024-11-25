package main

import (
	"bufio"
	"fmt"
	"log"
	"net/rpc"
	"os"
	"strings"
	"time"
)

func StartClient(filePath string) {
	client, err := rpc.Dial("tcp", "127.0.0.1:8000")
	if err != nil {
		log.Fatalf("Error dialing server: %s", err)
	}
	defer client.Close()

	// Open the requests file
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("Error opening file: %s", err)
	}
	defer file.Close()

	// Read the file line by line
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) != 3 {
			log.Printf("Invalid request format: %s", line)
			continue
		}

		args := RequestArgs{
			ClientID: parts[0],
			SeatID:   parts[1],
			Type:     parts[2],
		}

		var reply ServerResponse
		err = client.Call("ServerSession.RequestLock", args, &reply)
		if err != nil {
			log.Printf("Error calling server: %s", err)
			continue
		}

		if reply.Err != nil {
			log.Printf("Server response error: %s", reply.Err)
		} else {
			fmt.Printf("Server response: %s\n", reply.Message)
		}

		// Simulate delay between requests
		time.Sleep(1 * time.Second)

		// Send keep-alive ping to the server
		pingArgs := PingArgs{}
		var pingReply PingReply
		err = client.Call("ServerSession.Ping", pingArgs, &pingReply)
		if err != nil {
			log.Printf("Error pinging server: %s", err)
		} else {
			fmt.Println("Keepalive:", pingReply.Message)
		}

		// Timeout or retry logic if needed
	}
	if err := scanner.Err(); err != nil {
		log.Fatalf("Error reading file: %s", err)
	}
}
