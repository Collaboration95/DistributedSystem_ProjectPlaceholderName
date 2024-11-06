package server

import (
	"fmt"
	"log"
	"net"
	"net/rpc"
	"os"

	"github.com/Collaboration95/DistributedSystem_ProjectPlaceholderName.git/api"
	"github.com/Collaboration95/DistributedSystem_ProjectPlaceholderName.git/config"
	"github.com/Collaboration95/DistributedSystem_ProjectPlaceholderName.git/store"
)

// "github.com/Collaboration95/DistributedSystem_ProjectPlaceholderName.git/store"
type App struct {
	listener net.Listener

	// wrapper and manager for db instance
	store *store.Store

	logger *log.Logger

	// Current Node's Address
	address string

	// In-memory struct of handles.
	// Maps handle IDs to handle metadata.
	// handles map[int]Handle

	// In-memory struct of locks.
	// Maps filepaths to Lock structs.
	locks map[api.FilePath]*Lock

	// In-memory struct of sessions.
	sessions map[api.ClientID]*Session
}

// No choice but to make this variable package-level :(
var app *App

func Run(conf *config.Config) {
	var err error

	// Init app struct.
	app = &App{
		logger:   log.New(os.Stderr, "[server] ", log.LstdFlags),
		store:    store.New(conf.RaftDir, conf.RaftBind, conf.InMem),
		address:  conf.Listen,
		locks:    make(map[api.FilePath]*Lock),
		sessions: make(map[api.ClientID]*Session),
	}

	// Open the store.
	bootstrap := conf.Join == ""
	err = app.store.Open(bootstrap, conf.NodeID)
	if err != nil {
		log.Fatal(err)
	}

	if !bootstrap {
		// Set up TCP connection.
		client, err := rpc.Dial("tcp", conf.Join)
		if err != nil {
			log.Fatal(err)
		}

		app.logger.Printf("set up connection to %s", conf.Join)

		var req JoinRequest
		var resp JoinResponse

		req.RaftAddr = conf.RaftBind
		req.NodeID = conf.NodeID

		err = client.Call("Handler.Join", req, &resp)
		if err != nil {
			log.Fatal(err)
		}
		if resp.Error != nil {
			log.Fatal(err)
		}
	}

	// Listen for client connections.
	handler := new(Handler)
	err = rpc.Register(handler)

	app.listener, err = net.Listen("tcp", conf.Listen)
	app.logger.Printf("server listen in %s", conf.Listen)
	if err != nil {
		fmt.Println(err.Error())
	}

	// Accept connections.
	rpc.Accept(app.listener)
}
