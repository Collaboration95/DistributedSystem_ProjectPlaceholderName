// command line arg parsing:

// client ID is set via command line interface with a default a value

// flag.Parse() reads command - line flags allowing the client ID to be specified dynamically

// 2. SIGNAL HANDLING

// quitCh channel captures OS signals (eg. interrupt or kill signals)

// 3. SESSION INITIALIZATION

//client initializes a session with the lock service by calling client.InitSession(api.ClientID(acquireLock_clientID))

// 4. LOCK OPERATIONS

// open a lock at Lock/Lock1 using sess.OpenLock

// in a loop, client tries to acquire the lock with sess.TryAcquireLock("Lock/Lock1", api.EXCLUSIVE)

// if successful, it writes the Client ID to the lock's content
// after writing, reads back the lock's content to confirm ownership, which helps validate successful acquisition

// 5. Logging and Timing
// logs and timestamps are used to measure and print the time taken to acquire the lock

// 6. Exiting on Signal

// Related Files: client.go & api.go

// api.go
// ClientID and FilePath are provided in chubby/api.go
// LockMode constants are defined in api.go
// RPC Structures - RPC requests & response structures for interactions between the client & server in api.go
// Session Management: InitSessionRequest & InitSessionRequest allow the client to initialize session w server
// Lock Operations
// Reading & Writing

package main

import (
	"api"
	"client"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var acquireLock_clientID string // ID of this client.

func init() {
	flag.StringVar(&acquireLock_clientID, "acquireLock_clientID1", "acquireLock_clientID", "ID of this client2")
}

func main() {
	// parse flags from command line
	flag.Parse()

	quitCh := make(chan os.Signal, 1)
	signal.Notify(quitCh, os.Kill, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT)
	time.Sleep(5 * time.Second)
	sess, err := client.InitSession(api.ClientID(acquireLock_clientID))
	if err != nil {
		log.Fatal(err)
	}

	errOpenLock := sess.OpenLock("Lock/Lock1")
	if errOpenLock != nil {
		log.Fatal(errOpenLock)
	}
	startTime := time.Now()
	for {
		isSuccessful, err := sess.TryAcquireLock("Lock/Lock1", api.EXCLUSIVE)
		if err != nil {
			log.Println(err)
		}
		if isSuccessful && err == nil {
			isSuccessful, err = sess.WriteContent("Lock/Lock1", acquireLock_clientID)
			if !isSuccessful {
				fmt.Println("Unexpected Error Writing to Lock")
			}
			if err != nil {
				log.Fatal(err)
			}
			content, err := sess.ReadContent("Lock/Lock1")
			if err != nil {
				log.Fatal(err)
			} else {
				fmt.Printf("Read Content is %s\n", content)

			}
			if content == acquireLock_clientID {
				elapsed := time.Since(startTime)
				log.Printf("Successfully acquired lock after %s \n", elapsed)
			}
			return
		}
	}

	content, err := sess.ReadContent("Lock/Lock1")
	if err != nil {
		log.Fatal(err)
	} else {
		fmt.Printf("Read Content is %s\n", content)
	}
	if content == acquireLock_clientID {
		elapsed := time.Since(startTime)
		log.Printf("Successfully acquired lock after %s\n", elapsed)
	}
	// exit on signal
	<-quitCh
}
