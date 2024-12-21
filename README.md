# Movie Booking Website

1. On a terminal, navigate to the server folder, run the following commands
   - cd server
   - go run server.go

2. On a separate terminal, navigate to the client folder, run the following commands
   - cd client
   - go run client.go

3. On terminal running client.go run the following command to reset all seats to available status between each iteration of scaling test
   -  `clean` 

5. On terminal running client.go run the following command to terminate client session 
   -  `done` 

# To book/cancel a seat

Enter your requests in the format 'clientID SeatID RequestType' (e.g., 'client1 1A RESERVE')

- clientID SEATID REQUESTType
- Example to reserve
   - client1 1A RESERVE
 <img width="320" alt="image" src="https://github.com/user-attachments/assets/745f13e5-18d3-496d-ac4e-34b253c1af2d" />

- Example to cancel
  - client1 1A CANCEL
<img width="323" alt="image" src="https://github.com/user-attachments/assets/6495a8a4-9a8e-46c7-8d65-8d807b679252" />

# Scalability Testing 

1. Before starting, please run the following command on `terminal running client.go` to reset all seat status to available between different test iterations.
   - clean 
3. Then on `terminal running client.go` run the following command for scaling number of concurrent requests where N is a integer between  1 - 175 (for concurrent requests)
   - scale N
  
eg : `scale 10`
You should see an output `Reserved 10 seats among random clients in 4.303833ms,` 

Between each iteration of `scale N` requests , reformat the data by doing `clean` (reset all seat status to available before each iteration of the scaling tests)

For example to test 10 requests followed by 20 requests, the commands to be run on client terminal will be:

1. clean
2. scale 10
3. clean
4. scale 20
5. clean

