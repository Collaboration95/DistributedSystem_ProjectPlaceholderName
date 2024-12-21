# Movie Booking Website

cd server
go run server.go

then

cd client
go run client.go --clientID=client1  #replace with clientID  like client2, client3


# To book a seat:

Enter your requests in the format 'SeatID RequestType' (e.g., '1A RESERVE')

<img width="319" alt="image" src="https://github.com/user-attachments/assets/f5775ba3-952c-44e0-abf1-e299050a6bed" />

# To simulate leader failure and election, uncomment 	

  // time.Sleep(20 * time.Second)

	// // Start the leader election process
	// // Simulate leader failure
	// fmt.Printf("\n****************************Simulating Leader Failure****************************\n")
	// leaderServer := getLeader(servers)

	// leaderServer.isAlive = false

# Scalability Testing

Please refer to Scalability_Testing branch
