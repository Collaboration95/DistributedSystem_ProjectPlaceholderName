# Movie Booking Website

1. On a terminal, navigate to the server folder, run the following commands
   - cd server
   - go run server.go

2. On a separate terminal, navigate to the client folder, run the following commands
   - cd client
   - go run client.go

3. On terminal running client.go run the following command to reset all seats to available status in seats.txt file
   -  `clean` 

5. On terminal running client.go run the following command to terminate client session 
   -  `done` 

# To book/cancel a seat

Enter your requests in the format 'clientID SeatID RequestType' (e.g., 'client1 1A RESERVE')

- clientID SEATID REQUESTType
- Example to reserve
   - client1 1A RESERVE
- Example to cancel
  - client1 1B CANCEL

# Scalability Testing 

Before starting , please enter `clean` to reformat the seats.txt on the server side .
This functionality has been added to make the scalability testing process easier.

Then enter `scale N` Where N is a integer between  1 - 175 (for concurrent requests)
eg : `scale 10`
You should see an output `Reserved 10 seats among random clients in 4.303833ms,` 

Between `scale N` requests , reeformat the data by doing `clean`

