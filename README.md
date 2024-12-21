# Movie Booking Website

1. On a terminal, navigate to the server folder, run the following commands
   - cd server
   - go run server.go

2. On a separate terminal, navigate to the client folder, run the following commands
   - cd client
   - go run client.go

# To book a seat

Enter your requests in the format 'SeatID RequestType' (e.g., '1A RESERVE')

- clientID SEATID ACTION
eg: client1 1A RESERVE
    client1 1B CANCEL

# Scalability Testing 

Before starting , please enter `clean` to reformat the seats.txt on the server side .
This functionality has been added to make the scalability testing process easier.

Then enter `scale N` Where N is a integer between  1 - 175 (for concurrent requests)
eg : `scale 10`
You should see an output `Reserved 10 seats among random clients in 4.303833ms,` 

Between `scale N` requests , reeformat the data by doing `clean`

