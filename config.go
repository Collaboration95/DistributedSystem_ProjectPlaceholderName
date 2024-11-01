package main

import "time"

const (
	serverCount = 3

	port = "8080"

	ElectionTimeoutMin = 150
	ElectionTimeoutMax = 300
	HeartbeatInterval  = 50 * time.Millisecond
	ProgramTimeOut     = 5 * time.Second
)
