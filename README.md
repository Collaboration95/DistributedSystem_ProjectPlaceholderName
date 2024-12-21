# Movie Booking Website

1. On a terminal, navigate to the server folder, run the following commands
   - cd server
   - go run server.go

2. On a separate terminal, navigate to the client folder, run the following commands
   - cd client
   - go run client.go --clientID=client1  #replace with clientID  like client2, client3


# To book/cancel a seat

Enter your requests in the format 'SeatID RequestType' (e.g., '1A RESERVE')

- 1A RESERVE

<img width="319" alt="image" src="https://github.com/user-attachments/assets/f5775ba3-952c-44e0-abf1-e299050a6bed" />

Example to cancel:

- 1A CANCEL

# Simulate leader failure
### To simulate leader failure and election, uncomment lines 905 to 911 shown below 
### These lines are currently commented out to not simulate leader failure

	// time.Sleep(20 * time.Second)
	// // Start the leader election process
	// // Simulate leader failure
	// fmt.Printf("\n****************************Simulating Leader Failure****************************\n")
	// leaderServer := getLeader(servers)

	// leaderServer.isAlive = false

# Scalability Testing

Please refer to Scalability_Testing branch
