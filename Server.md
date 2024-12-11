# Server Functions
1. Init Server
    a. func initserver

2. Client - Server
    a. func createsession
    b. func serversession
    c. func processrequest
    d. func processqueue
    e. func getclientIDfromSessionID

3. RAFT Election
    a. func handleheartbeat
    b. func GetLeaderIP
    c. func initLoadBalancer
    d. func updateLoadBalancer

4. Main
    a. func main

5. Seats.txt
    a. func loadseats
    b. func saveseats

6. RAFT Log Replication
    a. func AppendEntries
    b. func SendAppendEntries
    c. func CommitEntries
    d. func ApplyLogEntries

- Client Booked Seats
- Leader add as entry in the local log
- Leader replicate log to follower
- Leader wait for majority of follower to write in local log
- Leader commit
- tell follower it is comitted
- follower commit