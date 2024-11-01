package main

import (
	"fmt"
)

// Booking represents the details of a movie ticket booking
type Booking struct {
	MovieTicketName string
	MovieTicketID   string
	Location        string
	Date            string
	TimeSlot        string
	SeatNumber      string
	ClientID        int
}

// ChubbyClient represents a client that can interact with Chubby cells
type ChubbyClient struct {
	ID          int
	BookingInfo *Booking
}

// CheckLeader checks the status of the Chubby cell leader
func (client *ChubbyClient) CheckLeader(cells []*ChubbyCell) (*ChubbyCell, error) {
	for _, cell := range cells {
		if cell.CheckLeader() {
			return cell, nil // Return the ChubbyCell instance
		}
	}
	return nil, fmt.Errorf("No leader found")
}

// AcquireLock attempts to acquire a lock from the Chubby cell
func (client *ChubbyClient) AcquireLock(cell *ChubbyCell) (bool, error) {
	lockMessage := &Message{Content: "Lock for Booking"}
	if cell.AcquireLock(lockMessage, client.ID) { // Pass client ID
		return true, nil
	}
	return false, fmt.Errorf("Failed to acquire lock from Chubby Cell %d", cell.ID)
}

// ReleaseLock releases the lock held by the client
func (client *ChubbyClient) ReleaseLock(cell *ChubbyCell) {
	cell.ReleaseLock(client.ID) // Pass client ID
	fmt.Printf("Client %d: Released lock from Chubby Cell %d\n", client.ID, cell.ID)
}

// ReadAvailableTickets retrieves information from the Chubby cell without a lock
func (client *ChubbyClient) ReadAvailableTickets(cell *ChubbyCell) {
	fmt.Printf("Client %d: Reading available tickets from cell %d\n", client.ID, cell.ID)
}

// WriteBooking sends booking information to the Chubby cell with a lock
func (client *ChubbyClient) WriteBooking(cell *ChubbyCell, booking *Booking) error {
	if ok, err := client.AcquireLock(cell); !ok || err != nil {
		return err
	}
	defer client.ReleaseLock(cell)

	fmt.Printf("Client %d: Writing booking info to cell %d: %+v\n", client.ID, cell.ID, booking)
	return nil
}
