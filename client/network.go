package client

// assumptions the client makes about addresses of Chubby servers

// we assume that any chubby node must have one of these addresses
// may modify this

var PossibleServerAddrs = map[string]bool{
	"172.20.128.1:5379": true,
	"172.20.128.2:5379": true,
	"172.20.128.3:5379": true,
	"172.20.128.4:5379": true,
	"172.20.128.5:5379": true,
}
