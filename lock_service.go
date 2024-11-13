package main

import (
	"fmt"
	"sync"
)

type LockService struct {
	Locks     map[string]string   // LockName -> ClientID
	Seats     map[string]LockMode //Map seat IDs to lock status
	LockLimit int
	mutex     sync.Mutex
}

func NewLockService() *LockService {
	ls := &LockService{
		Locks:     make(map[string]string),
		Seats:     make(map[string]LockMode),
		LockLimit: 5,
	}
	for i := 1; i <= 50; i++ {
		ls.Seats[fmt.Sprintf("seat-%d", i)] = Free
	}
	return ls
}

func (ls *LockService) AcquireLock(req LockRequest) LockResponse {
	if owner, exists := ls.Locks[req.LockName]; exists {
		return LockResponse{
			Success: false,
			Message: fmt.Sprintf("Lock is already held by %s", owner),
		}
	}
	ls.Locks[req.LockName] = req.ClientID
	return LockResponse{
		Success: true,
		Message: "Lock acquired",
	}
}

func (ls *LockService) ReleaseLock(req LockRequest) LockResponse {
	if owner, exists := ls.Locks[req.LockName]; exists {
		if owner == req.ClientID {
			delete(ls.Locks, req.LockName)
			return LockResponse{
				Success: true,
				Message: "Lock released",
			}
		}
		return LockResponse{
			Success: false,
			Message: "You do not own the lock",
		}
	}
	return LockResponse{
		Success: false,
		Message: "Lock does not exist",
	}
}

// RequestLock handles lock requests from clients
func (ls *LockService) RequestLock(args RequestLockArgs, reply *RequestLockResponse) error {
	ls.mutex.Lock()
	defer ls.mutex.Unlock()

	seatID := args.SeatID
	clientID := args.ClientID

	// Check if the seat is already locked or booked
	if currentStatus, exists := ls.Seats[seatID]; !exists || currentStatus == Booked {
		reply.Success = false
		reply.Message = fmt.Sprintf("%s is already booked or unavailable", seatID)
		return nil
	}

	// Check if seat is reserved by another client
	if lockHolder, exists := ls.Locks[seatID]; exists && lockHolder != clientID {
		reply.Success = false
		reply.Message = fmt.Sprintf("%s is already reserved by another client", seatID)
		return nil
	}

	// Reserve the seat and assign lock to the client
	ls.Seats[seatID] = Reserved
	ls.Locks[seatID] = clientID
	reply.Success = true
	reply.Message = fmt.Sprintf("%s reserved successfully", seatID)
	return nil
}

// BookSeat confirms the reservation by marking a seat as booked
func (ls *LockService) BookSeat(args RequestLockArgs, reply *RequestLockResponse) error {
	ls.mutex.Lock()
	defer ls.mutex.Unlock()

	seatID := args.SeatID
	clientID := args.ClientID

	// Ensure the seat is currently reserved by the requesting client
	if currentStatus, exists := ls.Seats[seatID]; !exists || currentStatus != Reserved || ls.Locks[seatID] != clientID {
		reply.Success = false
		reply.Message = fmt.Sprintf("%s is not reserved by you or is unavailable", seatID)
		return nil
	}

	// Book the seat
	ls.Seats[seatID] = Booked
	delete(ls.Locks, seatID)
	reply.Success = true
	reply.Message = fmt.Sprintf("%s booked successfully", seatID)
	return nil
}

// RemoveLock remove a lock on a seat if it's in a reserved state
func (ls *LockService) RemoveLock(args RequestLockArgs, reply *RequestLockResponse) error {
	ls.mutex.Lock()
	defer ls.mutex.Unlock()

	seatID := args.SeatID
	clientID := args.ClientID

	// Ensure the seat is currently reserved by the requesting client
	if currentStatus, exists := ls.Seats[seatID]; !exists || currentStatus != Reserved || ls.Locks[seatID] != clientID {
		reply.Success = false
		reply.Message = fmt.Sprintf("%s is not reserved by you or is unavailable", seatID)
		return nil
	}

	// Release the lock
	ls.Seats[seatID] = Free
	delete(ls.Locks, seatID)
	reply.Success = true
	reply.Message = fmt.Sprintf("%s lock released", seatID)
	return nil
}

// OLD'OVEN CODE (TEMP)
// // KeepAlive handles keep-alive requests from clients
// func (node *ChubbyNode) KeepAlive(args KeepAliveArgs, reply *KeepAliveResponse) error {
// 	node.mutex.Lock()
// 	defer node.mutex.Unlock()

// 	// Update the last active time for the client
// 	node.clients[args.ClientID] = time.Now()
// 	reply.Success = true
// 	reply.Message = "Keep-alive acknowledged"
// 	return nil
// }
