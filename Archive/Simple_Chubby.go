package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// Message represents a lock request message
type Message struct {
	MovieTicketID string
	SeatNumber    string
	AliveTime     time.Duration
	ClientID      int
}

// ChubbyCell represents a single cell in the Chubby cluster
type ChubbyCell struct {
	ID                 int
	IsAlive            bool
	IsLeader           bool
	ElectionInProgress bool
	CurrentLock        *Message
	mutex              sync.Mutex
}

// ChubbyClient represents a client that can interact with Chubby cells
type ChubbyClient struct {
	ID       int
	Leader   *ChubbyCell
	LastLock *Message
}

// ChubbyCluster manages the collection of Chubby cells
type ChubbyCluster struct {
	Cells   []*ChubbyCell
	Clients []*ChubbyClient
	mutex   sync.Mutex
}

// NewChubbyCluster initializes a new Chubby cluster
func NewChubbyCluster() *ChubbyCluster {
	cluster := &ChubbyCluster{
		Cells:   make([]*ChubbyCell, 5),
		Clients: make([]*ChubbyClient, 3),
	}

	// Initialize cells
	for i := 0; i < 5; i++ {
		cluster.Cells[i] = &ChubbyCell{
			ID:                 i,
			IsAlive:            true,
			IsLeader:           i == 0, // First cell is initially the leader
			ElectionInProgress: false,
		}
	}

	// Initialize clients
	for i := 0; i < 3; i++ {
		cluster.Clients[i] = &ChubbyClient{
			ID:     i,
			Leader: cluster.Cells[0],
		}
	}

	return cluster
}

// FindLeader helps a client find the current leader
func (c *ChubbyClient) FindLeader(cluster *ChubbyCluster) *ChubbyCell {
	for _, cell := range cluster.Cells {
		if cell.IsLeader && cell.IsAlive {
			c.Leader = cell
			return cell
		}
	}
	return nil
}

// AcquireLock attempts to acquire a lock from the leader
func (c *ChubbyClient) AcquireLock(msg Message) (bool, error) {
	if c.Leader == nil || !c.Leader.IsAlive {
		return false, fmt.Errorf("no leader available")
	}

	c.Leader.mutex.Lock()
	defer c.Leader.mutex.Unlock()

	if c.Leader.ElectionInProgress {
		return false, fmt.Errorf("election in progress")
	}

	if c.Leader.CurrentLock != nil {
		return false, fmt.Errorf("lock already held")
	}

	c.Leader.CurrentLock = &msg
	c.LastLock = &msg
	return true, nil
}

// ReleaseLock releases a previously acquired lock
func (c *ChubbyClient) ReleaseLock() (bool, error) {
	if c.Leader == nil || !c.Leader.IsAlive {
		return false, fmt.Errorf("no leader available")
	}

	c.Leader.mutex.Lock()
	defer c.Leader.mutex.Unlock()

	if c.Leader.ElectionInProgress {
		return false, fmt.Errorf("election in progress")
	}

	if c.Leader.CurrentLock == nil {
		return false, fmt.Errorf("no lock held")
	}

	c.Leader.CurrentLock = nil
	c.LastLock = nil
	return true, nil
}

// ExtendLock extends the alive time of a lock
func (c *ChubbyClient) ExtendLock(duration time.Duration) (bool, error) {
	if c.LastLock == nil {
		return false, fmt.Errorf("no lock to extend")
	}

	c.LastLock.AliveTime += duration
	return true, nil
}

// SimpleElection performs a simple leader election
func (cluster *ChubbyCluster) SimpleElection() {
	cluster.mutex.Lock()
	defer cluster.mutex.Unlock()

	// Mark all cells as being in election
	for _, cell := range cluster.Cells {
		cell.ElectionInProgress = true
		cell.IsLeader = false
	}

	// Find all alive cells
	var aliveCells []*ChubbyCell
	for _, cell := range cluster.Cells {
		if cell.IsAlive {
			aliveCells = append(aliveCells, cell)
		}
	}

	if len(aliveCells) > 0 {
		// Randomly select a new leader from alive cells
		newLeader := aliveCells[rand.Intn(len(aliveCells))]
		newLeader.IsLeader = true
	}

	// Election complete
	for _, cell := range cluster.Cells {
		cell.ElectionInProgress = false
	}
}

// SimulateFailure simulates a cell failure
func (cell *ChubbyCell) SimulateFailure() {
	cell.mutex.Lock()
	defer cell.mutex.Unlock()
	cell.IsAlive = false
}

// func main() {
// 	// Test scenario
// 	cluster := NewChubbyCluster()

// 	// Client 1 acquires lock
// 	msg1 := Message{
// 		MovieTicketID: "T123",
// 		SeatNumber:    "A1",
// 		AliveTime:     time.Second * 10,
// 		ClientID:      0,
// 	}

// 	success, err := cluster.Clients[0].AcquireLock(msg1)
// 	fmt.Printf("Client 1 lock acquisition: %v, err: %v\n", success, err)

// 	time.Sleep(time.Second * 2)

// 	// Client 1 releases lock
// 	success, err = cluster.Clients[0].ReleaseLock()
// 	fmt.Printf("Client 1 lock release: %v, err: %v\n", success, err)

// 	// Client 2 acquires lock
// 	msg2 := Message{
// 		MovieTicketID: "T124",
// 		SeatNumber:    "B2",
// 		AliveTime:     time.Second * 10,
// 		ClientID:      1,
// 	}

// 	success, err = cluster.Clients[1].AcquireLock(msg2)
// 	fmt.Printf("Client 2 lock acquisition: %v, err: %v\n", success, err)

// 	// Simulate leader failure
// 	cluster.Clients[1].Leader.SimulateFailure()
// 	fmt.Println("Leader failure simulated")

// 	// Client 2 extends lock during failure
// 	success, err = cluster.Clients[1].ExtendLock(time.Second * 5)
// 	fmt.Printf("Client 2 lock extension: %v, err: %v\n", success, err)

// 	// New leader election
// 	cluster.SimpleElection()
// 	fmt.Println("New leader elected")

// 	// Client 2 finds new leader
// 	newLeader := cluster.Clients[1].FindLeader(cluster)
// 	fmt.Printf("Client 2 found new leader: %v\n", newLeader != nil)

// 	// Client 2 releases lock with new leader
// 	success, err = cluster.Clients[1].ReleaseLock()
// 	fmt.Printf("Client 2 lock release: %v, err: %v\n", success, err)
// }
