package main

import (
	"fmt"
	"net"
	"net/rpc"
)

// StartRPCServer sets up the RPC server and starts listening for connections
func StartRPCServer(port string, lockService *LockService) {
	err := rpc.Register(lockService)
	if err != nil {
		fmt.Println("Failed to register LockService:", err)
		return
	}

	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		fmt.Println("Failed to start listener:", err)
		return
	}
	defer listener.Close()

	fmt.Printf("RPC Server is running on port %s\n", port)
	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Failed to accept connection:", err)
			continue
		}
		go rpc.ServeConn(conn)
	}
}
