package raft

import ()

// msg

type msgFrom struct {
	name uint64
}

type msgInfo struct {
	msg string
}

type msgElec struct {
	lead uint64
	term uint64
}

type msg struct {
	from msgFrom
	info msgInfo
	elec msgElec
}

type Msg = msg

const (
	MsgVoteMe    string = "vote_me"
	MsgHeartbeat string = "heartbeat"
)

const (
	ResMsgOk  string = "OK"
	ResMsgNot string = "not"
)
