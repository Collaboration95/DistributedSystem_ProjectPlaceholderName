package main

import (
	"fmt"
	"math/rand"
	"time"
)

// Assume the required types and methods are defined (ChubbyCell, Message, etc.)

func main() {
	rand.NewSource(time.Now().UnixNano())

	// Create a set of Chubby cells
	cells := []*ChubbyCell{
		NewChubbyCell(1),
		NewChubbyCell(2),
		NewChubbyCell(3),
	}

	// Create client 1
	client1 := &ChubbyClient{
		ID: 1,
		BookingInfo: &Booking{
			MovieTicketName: "Panda Plan\n",
			MovieTicketID:   "0001234\n",
			Location:        "GV Plaza\n",
			Date:            "2024-10-29\n",
			TimeSlot:        "19:00\n",
			SeatNumber:      "A1\n",
			ClientID:        1,
		},
	}

	// Create client 2
	client2 := &ChubbyClient{
		ID: 2,
		BookingInfo: &Booking{
			MovieTicketName: "Look Back\n",
			MovieTicketID:   "0001892\n",
			Location:        "The Projector @Cineleisure\n",
			Date:            "2024-11-01\n",
			TimeSlot:        "16:00\n",
			SeatNumber:      "B3\n",
			ClientID:        2,
		},
	}

	// Leader election
	for _, cell := range cells {
		cell.Elect(cells)
	}

	// Check leader
	leaderCell, err := client1.CheckLeader(cells)
	if err != nil {
		fmt.Printf("Client %d: Error checking Chubby Cell Leader: %v\n", client1.ID, err)
		return
	}

	leaderID := leaderCell.ID // Get the leader's ID
	fmt.Printf("Client %d: Current Chubby Cell Leader: %d\n", client1.ID, leaderID)

	// Read available tickets with Client 1
	client1.ReadAvailableTickets(leaderCell)

	// Acquire lock through the leader cell
	lockMessage := &Message{Content: "Lock for Booking"}
	if leaderCell.AcquireLock(lockMessage, client1.ID) {
		// Client 1 writes booking information to the leader cell
		fmt.Printf("Client %d: Acquired lock on Chubby Cell Leader %d\n", client1.ID, leaderCell.ID)
		fmt.Printf("Client %d: Writing booking info to Chubby Cell Leader %d: \n %+v \n", client1.ID, leaderCell.ID, client1.BookingInfo)
	} else {
		fmt.Printf("Client %d: Failed to acquire lock on Chubby Cell Leader %d\n", client1.ID, leaderCell.ID)
	}

	// Now Client 2 tries to acquire the same lock while Client 1 holds it
	if leaderCell.AcquireLock(lockMessage, client2.ID) {
		fmt.Printf("Client %d: Acquired lock on Chubby Cell Leader %d\n", client2.ID, leaderCell.ID)
	} else {
		fmt.Printf("Client %d: Error acquiring lock on Chubby Cell Leader %d as lock is held by another cell\n", client2.ID, leaderCell.ID)
	}

	// Client 1 releases the lock
	leaderCell.ReleaseLock(client1.ID)
	fmt.Printf("Client %d: Released lock on Chubby Cell Leader %d\n", client1.ID, leaderCell.ID)

	// Simulate leader failure
	fmt.Printf("Simulating failure of the Chubby Cell Leader (Cell %d)\n", leaderID)
	for _, cell := range cells {
		if cell.ID == leaderID {
			cell.SimulateFailure()
		}
	}

	// Show that KeepAlive is working for active cells
	for _, cell := range cells {
		cell.KeepAlive()
	}

	// Re-elect a new leader since the previous leader is down
	for _, cell := range cells {
		if cell.IsAlive {
			cell.Elect(cells)
		}
	}

	// Check for the new leader after re-election
	newLeaderCell, err := client2.CheckLeader(cells)
	if err != nil {
		fmt.Printf("Client %d: Error checking new Chubby Cell Leader: %v\n", client2.ID, err)
	} else {
		fmt.Printf("Client %d: New Chubby Cell Leader after re-election: %d\n", client2.ID, newLeaderCell.ID)
	}

	// Client 2 attempts to acquire lock through the new leader
	if newLeaderCell != nil {
		if newLeaderCell.AcquireLock(lockMessage, client2.ID) {
			fmt.Printf("Client %d: Successfully acquired lock on new Chubby Cell Leader %d\n", client2.ID, newLeaderCell.ID)
			newLeaderCell.ReleaseLock(client2.ID)
			fmt.Printf("Client %d: Released lock on new Chubby Cell Leader %d\n", client2.ID, newLeaderCell.ID)
		} else {
			fmt.Printf("Client %d: Error acquiring lock on Chubby Cell Leader %d as lock is held by another cell\n", client2.ID, newLeaderCell.ID)
		}
	}
}
