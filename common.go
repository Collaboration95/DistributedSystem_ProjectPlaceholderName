package main

// RequestArgs defines the arguments needed for a request.
type RequestArgs struct {
	ClientID string
	SeatID   string
	Type     string
}

// ServerResponse defines the response to send back to the client.
type ServerResponse struct {
	Err     error
	Message string
}

// PingArgs is the structure for the ping request (empty, just for type consistency)
type PingArgs struct{}

// PingReply is the structure for the ping response (contains a message)
type PingReply struct {
	Message string
}
