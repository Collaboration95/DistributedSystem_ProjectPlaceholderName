# Movie Booking Website

cd server
go run server.go

then

cd client
go run client.go 

Before starting server.go , clean the seats.txt by pasting this : 

1G: available: 
2B: available: 
4G: available: 
2C: available: 
1A: available: 
1B: available: 
3C: available: 
3E: available: 
3F: available: 
1E: available: 
4E: available: 
5E: available: 
5G: available: 
3D: available: 
2G: available: 
5F: available: 
1F: available: 
5C: available: 
4F: available: 
3B: available: 
5A: available: 
1C: available: 
4D: available: 
3G: available: 
5D: available: 
3A: available: 
4C: available: 
4B: available: 
1D: available: 
2A: available: 
2E: available: 
2F: available: 
4A: available: 
2D: available: 
5B: available: 

After which , requests are of the following format clientID seatID Operation eg: client1 1A RESERVE
To test scalability , first clean the database by typing in `clean` on the client terminal. 
Then type in `scale 5` or any number between 1 & 47 ( randomly books n seats with n clients)