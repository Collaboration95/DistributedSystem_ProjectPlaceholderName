package main

type Server struct {
	ID       int
	Peers    []*Server
	RaftNode *RaftNode
	LockSvc  *LockService
	QuitChan chan bool
}

func (s *Server) Run() {
	s.RaftNode.Run()
}
