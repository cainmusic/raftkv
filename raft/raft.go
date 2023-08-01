package raft

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// node

type nodeBase struct {
	name    uint64
	cluster []uint64
	logger  *logger
	eTimer  *time.Timer
	hTimer  *time.Timer
}

type nodeComm struct {
	msgChanIn   chan msg
	msgChanOut  chan msg
	funcSendMsg func(uint64, uint64, msg) msg
}

type role uint64

type nodeElec struct {
	mutex     sync.Mutex
	term      uint64
	role      role
	lead      uint64
	voteCount uint64
	voteFor   uint64
}

type node struct {
	base nodeBase
	comm nodeComm
	elec nodeElec
}

type Node = node

const (
	follower  role = 1
	candidate role = 2
	leader    role = 3
)

const (
	timeoutTimes                       = 10 // 默认1，debug可以调到10，所有timeout慢10倍
	electionTimeoutBase  time.Duration = 100 * timeoutTimes * time.Millisecond
	electionTimeoutRange time.Duration = 100 * timeoutTimes * time.Millisecond
	heartbeatTimeout     time.Duration = 20 * timeoutTimes * time.Millisecond
)

func init() {
	initDebugLog()
}

func initDebugLog() {
	fmt.Printf("election timeout, base : %v, range : %v\n", electionTimeoutBase, electionTimeoutRange)
	fmt.Println("heartbeat :", heartbeatTimeout)
	fmt.Println("timeout times", timeoutTimes)
}

func New(name uint64, cluster []uint64) (*node, error) {
	np := &node{
		base: nodeBase{
			name:    name,
			cluster: cluster,
		},
		comm: nodeComm{
			msgChanIn:  make(chan msg),
			msgChanOut: make(chan msg),
		},
		elec: nodeElec{
			term:      0,
			role:      follower,
			lead:      0,
			voteCount: 0,
			voteFor:   0,
		},
	}

	np.initLogger(name) // 失败会panic
	np.initTimer()

	np.logln("============new node entity created============")
	np.logf("initing node %v, cluster %v\n", name, cluster)

	return np, nil
}

func (np *node) initLogger(name uint64) {
	logger, err := NewLogger(name)
	if err != nil {
		panic(err)
	}
	np.base.logger = logger
}

func (np *node) initTimer() {
	np.base.eTimer = time.NewTimer(time.Second)
	np.eTimerStop()
	np.base.hTimer = time.NewTimer(time.Second)
	np.hTimerStop()
}

func (np *node) GetName() uint64 {
	return np.base.name
}

func (np *node) GetMsgChan() (chan msg, chan msg) {
	return np.comm.msgChanIn, np.comm.msgChanOut
}

func (np *node) SetFuncSendMsg(f func(uint64, uint64, msg) msg) {
	np.comm.funcSendMsg = f
}

func (np *node) Start() {
	np.logln("node start")

	np.logln("go tick election")
	go np.tick()
}

func (np *node) eTimerSet() {
	et := randomElectionTimeout()
	np.logln("election timeout:", et)
	timerReset(np.base.eTimer, et)
}

func (np *node) eTimerStop() {
	timerStop(np.base.eTimer)
}

func (np *node) hTimerSet() {
	np.logln("heartbeat timeout:", heartbeatTimeout)
	timerReset(np.base.hTimer, heartbeatTimeout)
}

func (np *node) hTimerStop() {
	timerStop(np.base.hTimer)
}

func (np *node) tick() {
	eTimer := np.base.eTimer
	hTimer := np.base.hTimer
	np.eTimerSet()
	np.logln("tick start")
	for {
		select {
		case <-eTimer.C:
			np.logln("election timeout")
			np.becomeCandidate()
		case <-hTimer.C:
			np.logln("heartbeat timeout")
			np.sendHeartbeatRequests()
			np.hTimerSet()
		case msg := <-np.comm.msgChanIn:
			np.logln("got message in")
			np.handleMsgIn(msg)
		}
	}
}

func randomElectionTimeout() time.Duration {
	return time.Duration(rand.Intn(int(electionTimeoutRange))) + electionTimeoutBase
}

func (np *node) becomeFollower() {
	np.elec.mutex.Lock()
	defer np.elec.mutex.Unlock()
	// 变角色
	np.elec.role = follower
	np.hTimerStop() // 从leader回归follower时需要停heartbeat
	np.eTimerSet()  // follower永远骚动
}

func (np *node) becomeCandidate() {
	np.elec.mutex.Lock()
	defer np.elec.mutex.Unlock()
	// 变角色，加任期，投自己
	np.logln("becoming candidate")
	np.elec.role = candidate
	np.elec.term++
	np.elec.voteFor = np.base.name
	np.gotVote(true)
	// 发投票请求
	np.logln("send vote requests")
	np.sendVoteRequests()
}

func (np *node) becomeLeader(fLock bool) {
	if !fLock {
		np.elec.mutex.Lock()
		defer np.elec.mutex.Unlock()
	}
	if np.elec.role == leader {
		np.logln("already leader")
		return
	}
	np.logln("becoming leader")
	np.elec.role = leader
	np.elec.lead = np.base.name
	np.eTimerStop()
	np.hTimerSet()
}

func (np *node) voteForName(n uint64) {
	np.elec.mutex.Lock()
	defer np.elec.mutex.Unlock()
	np.elec.voteFor = n
}

func (np *node) gotVote(fLock bool) {
	if !fLock {
		np.elec.mutex.Lock()
		defer np.elec.mutex.Unlock()
	}
	np.logln("got vote")
	np.elec.voteCount++
	if int(np.elec.voteCount)*2 > len(np.base.cluster) {
		np.logln("vote from more then half")
		np.becomeLeader(true)
	}
}

func (np *node) handleElec(m msg) bool {
	np.elec.mutex.Lock()
	defer np.elec.mutex.Unlock()
	// 更新term和相关信息
	np.logln("handle message election info")
	if np.elec.term < m.elec.term {
		np.logln("jumping election term")
		np.elec.role = follower
		np.elec.term = m.elec.term
		np.elec.voteCount = 0
		np.elec.voteFor = 0
		if m.elec.lead != 0 {
			np.elec.lead = m.elec.lead
		}
		return true
	}
	return false
}

func (np *node) sendRequests(f func(uint64)) {
	if len(np.base.cluster) == 1 {
		np.logln("skip send vote request")
		return
	}
	for _, nodeName := range np.base.cluster {
		if nodeName != np.base.name {
			go f(nodeName)
		}
	}
}

func (np *node) sendRequestTo(dnn uint64, m msg, mt string) {
	m = np.makeMsg(mt)
	np.logf("request to: %v, type: %v, message: %v\n", dnn, mt, m)
	res := np.comm.funcSendMsg(np.base.name, dnn, m)
	np.logf("request response from: %v, response: %v\n", dnn, res)
	np.handleResMsg(res, mt)
}

func (np *node) sendVoteRequests() {
	np.sendRequests(np.sendVoteRequestTo)
}

func (np *node) sendVoteRequestTo(dnn uint64) {
	msg := np.makeMsg(MsgVoteMe)
	np.sendRequestTo(dnn, msg, MsgVoteMe)
}

func (np *node) sendHeartbeatRequests() {
	np.sendRequests(np.sendHeartbeatRequestTo)
}

func (np *node) sendHeartbeatRequestTo(dnn uint64) {
	msg := np.makeMsg(MsgHeartbeat)
	np.sendRequestTo(dnn, msg, MsgHeartbeat)
}

func (np *node) handleMsgIn(m msg) {
	res := np.handleMsg(m)
	np.comm.msgChanOut <- res
}

func (np *node) handleMsg(m msg) msg {
	np.logln("handle message:", m)
	fTerm := np.handleElec(m)
	switch m.info.msg {
	case MsgVoteMe:
		return np.handleMsgVoteMe(m, fTerm)
	case MsgHeartbeat:
		return np.handleMsgHeartbeat(m, fTerm)
	default:
		np.logln(m)
	}
	return msg{}
}

func (np *node) handleMsgVoteMe(m msg, fTerm bool) msg {
	// 若term有变化，则vote
	// 若term无变化，检车voteFor，若已vote过，不vote，否则vote
	if !fTerm && np.elec.voteFor != 0 {
		return np.makeMsg(ResMsgNot)
	}
	np.voteForName(m.from.name)
	np.eTimerSet()
	return np.makeMsg(ResMsgOk)
}

func (np *node) handleMsgHeartbeat(m msg, fTerm bool) msg {
	// 若term有变化，则无需额外处理
	// 若term无变化，检查role
	//   若是leader，则出现bug现象，但需要进行恢复，恢复的方法就是不接受安抚，以成为下一任candidate
	//   若是candidate，则becomeFollower
	//   若是follower，则不管
	if !fTerm && np.elec.role == leader {
		return np.makeMsg(ResMsgNot)
	}
	if np.elec.role == candidate {
		np.becomeFollower()
	}
	np.eTimerSet()
	return np.makeMsg(ResMsgOk)
}

func (np *node) makeMsg(info string) msg {
	return msg{
		from: msgFrom{name: np.base.name},
		info: msgInfo{msg: info},
		elec: msgElec{
			lead: np.elec.lead,
			term: np.elec.term,
		},
	}
}

func (np *node) handleResMsg(r msg, mt string) {
	np.logln("handle response message:", r)
	_ = np.handleElec(r)
	switch mt {
	case MsgVoteMe: // 请求投票类
		np.handleResMsgVoteMe(r)
	default:
		np.logln(r)
	}
}

func (np *node) handleResMsgVoteMe(r msg) {
	switch r.info.msg {
	case ResMsgOk: // 投了
		np.gotVote(false)
	}
}
