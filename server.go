package main

import (
	"fmt"
	"sync"
)

type Message struct {
	Content string
}

// ChubbyCell represents a single Chubby cell in the system
type ChubbyCell struct {
	ID                 int
	IsAlive            bool
	IsLeader           bool
	ElectionInProgress bool
	CurrentLock        *Message
	mutex              sync.Mutex
}

// NewChubbyCell initializes a new Chubby cell
func NewChubbyCell(id int) *ChubbyCell {
	return &ChubbyCell{
		ID:      id,
		IsAlive: true,
	}
}

// Elect conducts a leader election among Chubby cells
func (cell *ChubbyCell) Elect(cells []*ChubbyCell) {
	cell.mutex.Lock()
	defer cell.mutex.Unlock()

	if cell.ElectionInProgress {
		fmt.Printf("Chubby Cell %d: Election already in progress\n", cell.ID)
		return
	}

	cell.ElectionInProgress = true
	fmt.Printf("Chubby Cell %d: Starting election\n", cell.ID)

	var leaderCell *ChubbyCell
	for _, c := range cells {
		if c.IsAlive && (leaderCell == nil || c.ID > leaderCell.ID) {
			leaderCell = c
		}
	}

	if leaderCell != nil {
		leaderCell.IsLeader = true
		fmt.Printf("Chubby Cell %d: is the leader\n", leaderCell.ID)
	} else {
		fmt.Printf("Chubby Cell %d: No alive cells to elect\n", cell.ID)
	}

	cell.ElectionInProgress = false
}

// AcquireLock attempts to acquire a lock for the current cell (should only be called by the leader)
func (cell *ChubbyCell) AcquireLock(lock *Message, clientID int) bool {
	cell.mutex.Lock()
	defer cell.mutex.Unlock()

	if cell.CurrentLock == nil {
		cell.CurrentLock = lock
		fmt.Printf("Chubby Cell %d: Acquired lock by Client %d\n", cell.ID, clientID)
		return true
	}

	fmt.Printf("Chubby Cell %d: Lock already held by another cell\n", cell.ID)
	return false
}

// ReleaseLock releases the currently held lock (should only be called by the leader)
func (cell *ChubbyCell) ReleaseLock(clientID int) {
	cell.mutex.Lock()
	defer cell.mutex.Unlock()

	if cell.CurrentLock != nil {
		fmt.Printf("Chubby Cell %d: Released lock by Client %d\n", cell.ID, clientID)
		cell.CurrentLock = nil
	} else {
		fmt.Printf("Chubby Cell %d: No lock to release\n", cell.ID)
	}
}

// CheckLeader checks if the current cell is the leader
func (cell *ChubbyCell) CheckLeader() bool {
	return cell.IsLeader
}

// SimulateFailure simulates a failure of the cell
func (cell *ChubbyCell) SimulateFailure() {
	cell.mutex.Lock()
	defer cell.mutex.Unlock()

	cell.IsAlive = false
	cell.IsLeader = false
	cell.CurrentLock = nil
	fmt.Printf("Chubby Cell %d: Simulated failure\n", cell.ID)
}

// Recover simulates recovering the cell from failure
func (cell *ChubbyCell) Recover() {
	cell.mutex.Lock()
	defer cell.mutex.Unlock()

	cell.IsAlive = true
	fmt.Printf("Chubby Cell %d: Recovered\n", cell.ID)
}

// KeepAlive sends a keepalive signal to maintain the cell's active status
func (cell *ChubbyCell) KeepAlive() {
	cell.mutex.Lock()
	defer cell.mutex.Unlock()

	if cell.IsAlive {
		fmt.Printf("Chubby Cell %d: Sending keepalive\n", cell.ID)
	} else {
		fmt.Printf("Chubby Cell %d: Cannot send keepalive, Chubby Cell is dead\n", cell.ID)
	}
}
