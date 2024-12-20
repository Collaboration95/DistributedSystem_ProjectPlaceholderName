package common

// Request represents a client request
type Request struct {
	ClientID string
	SeatID   string
	Type     string // e.g., "RESERVE"
	ServerID string // Server session handling the request
}

// Response represents the server's response
type Response struct {
	Status  string // e.g., "SUCCESS", "FAILURE"
	Message string
	Data    interface{} // Can hold a list of seats
}
