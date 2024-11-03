package main

import "time"

// LockMode represents the status of a seat.
type LockMode int

const (
	Free     LockMode = iota
	Reserved          // Seat is reserved but not yet booked
	Booked            // Seat is fully booked
)

// Default lease durations
const (
	DefaultLeaseDuration = 20 * time.Second
	JeopardyDuration     = 45 * time.Second
)

// Request and response structs for RPC calls
type RequestLockArgs struct {
	SeatID   string
	ClientID string
}

type RequestLockResponse struct {
	Success bool
	Message string
}

// KeepAlive request and response structs
type KeepAliveArgs struct {
	ClientID string
}

type KeepAliveResponse struct {
	Success bool
	Message string
}
