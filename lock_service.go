package main

import "fmt"

type LockService struct {
	Locks map[string]string // LockName -> ClientID
}

func NewLockService() *LockService {
	return &LockService{
		Locks: make(map[string]string),
	}
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
