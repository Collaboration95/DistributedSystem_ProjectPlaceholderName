package client

// assumptions the client makes about addresses of Chubby servers

// we assume that any chubby node must have one of these addresses
// may modify this

// var PossibleServerAddrs = map[string]bool{
// 	"172.20.128.1:5379": true,
// 	"172.20.128.2:5379": true,
// 	"172.20.128.3:5379": true,
// 	"172.20.128.4:5379": true,
// 	"172.20.128.5:5379": true,
// }

var PossibleServerAddrs = map[string]bool{
	"127.0.0.1:8002": true,
	"127.0.0.1:8003": true,
	"127.0.0.1:8004": true,
	"127.0.0.1:8001": true,
	"127.0.0.1:8000": true,
}
