# My Raft

本文档主旨是：  
了解raft算法，并实现一个简单的raft算法，并与etcd的raft算法进行学习比较。  

* 本次实现项目：[github.com/cainmusic/raftkv](https://github.com/cainmusic/raftkv)
* etcd的raft项目：[github.com/etcd-io/raft](https://github.com/etcd-io/raft)

## 了解Raft算法

查看`./raft.md`文档

## Etcd-Raft

etcd将raft单独解除耦合独立了出来，形成了Raft项目  

核心文件：

* raft.go
* node.go
* rawnode.go

### 状态类型：

``` go
type StateType uint64

const (
    StateFollower StateType = iota
    StateCandidate
    StateLeader
    StatePreCandidate
    numStates
)
```


我们看到raft中使用的StateType主要是

* StateFollower
* StateCandidate
* StateLeader

对应raft算法中的follower、candidate、leader  
注：`StatePreCandidate`和`numStates`暂时不用考虑，前者是Raft算法的一个扩展，而后者目前主要用于测试。

### Config：

``` go
type Config struct {
    ID uint64

    ElectionTick int
    HeartbeatTick int

    Storage Storage

    ...
}
```

参数解读：

* ID，raft中node的ID，不能为0
* ElectionTick，raft中的选举timeout，此处命名tick，用来控制新的选举周期的开启
* HeartbeatTick，raft中的心跳timeout，此处命名tick，用来控制leader对followe的周期请求
* Storage，存储器，用来存储数据
    * 类型Storage是一个接口，定义在`./storage.go`文件中
    * 你可以定义自己的存储器替换raft默认的存储器

### Raft

``` go
type raft struct {
    id uint64

    Term uint64
    Vote uint64

    readStates []ReadState

    raftLog *raftLog

    maxMsgSize         entryEncodingSize
    maxUncommittedSize entryPayloadSize
    prs tracker.ProgressTracker

    state StateType

    isLearner bool

    msgs []pb.Message
    msgsAfterAppend []pb.Message

    lead uint64
    leadTransferee uint64
    pendingConfIndex uint64
    disableConfChangeValidation bool
    uncommittedSize entryPayloadSize

    readOnly *readOnly

    electionElapsed int
    heartbeatElapsed int

    checkQuorum bool
    preVote     bool

    heartbeatTimeout int
    electionTimeout  int
    randomizedElectionTimeout int
    disableProposalForwarding bool
    stepDownOnRemoval         bool

    tick func()
    step stepFunc

    logger Logger

    pendingReadIndexMessages []pb.Message
}
```

参数解读：

* id，来自于Config.ID
* Term，选举任期，默认0
* Vote，当前选举任期的投票对象
    * 例如，成为candidate的时候会投票给自己，此时的Vote就等于自己的id
    * 再例，当自己是follower并且收到投票请求时，若通过判断可以投票，就更新为请求者的id
* readStates，只读请求状态缓存
    * 类型ReadState定义在`./read_only.go`文件中
    * 本文档暂不详细展开相关问题
* raftLog，节点日志
    * 类型raftLog定义在`./log.go`文件中
    * 是raft算法中`Log Replication`的核心结构
    * 初始化的时候存储器也放在其中
* state，状态
* lead，leader的id
* readOnly，参考`./read_only.go`文件
* electionElapsed，electionTimeout，randomizedElectionTimeout，选举timeout的消逝与过期，后面详解
* heartbeatElapsed，electionTimeout，心跳timeout的消逝与过期，后面详解
* disableProposalForwarding
* tick，核心业务处理方法，tickElection和tickHeartbeat
* step，message处理方法，stepFollower、stepCandidate、stepLeader以及外围的Step方法
* logger，一般日志

其他未提及字段暂时不展开说。

### newRaft

一系列初始化：

1. Config validate
2. raftlog
    * storage InitialState
3. r := &raft{...}
4. confchange Restore
    * assertConfStatesEquivalent
5. 初始化hs（hard state）
6. 初始化raftlog.applied
7. becomeFollower
8. logger.Infof

基本的初始化流程，后面的讲解从becomeFollower和它的几个兄弟方法开始

### becomeXxx

接下来的becomeXxx几个方法，会开始深入涉及election term的相关内容。

```go
func (r *raft) becomeFollower(term uint64, lead uint64) {
    r.step = stepFollower
    r.reset(term)
    r.tick = r.tickElection
    r.lead = lead
    r.state = StateFollower
    r.logger.Infof("%x became follower at term %d", r.id, r.Term)
}

func (r *raft) becomeCandidate() {
    // TODO(xiangli) remove the panic when the raft implementation is stable
    if r.state == StateLeader {
        panic("invalid transition [leader -> candidate]")
    }
    r.step = stepCandidate
    r.reset(r.Term + 1)
    r.tick = r.tickElection
    r.Vote = r.id
    r.state = StateCandidate
    r.logger.Infof("%x became candidate at term %d", r.id, r.Term)
}

func (r *raft) becomeLeader() {
    // TODO(xiangli) remove the panic when the raft implementation is stable
    if r.state == StateFollower {
        panic("invalid transition [follower -> leader]")
    }
    r.step = stepLeader
    r.reset(r.Term)
    r.tick = r.tickHeartbeat
    r.lead = r.id
    r.state = StateLeader
    // Followers enter replicate mode when they've been successfully probed
    // (perhaps after having received a snapshot as a result). The leader is
    // trivially in this state. Note that r.reset() has initialized this
    // progress with the last index already.
    pr := r.prs.Progress[r.id]
    pr.BecomeReplicate()
    // The leader always has RecentActive == true; MsgCheckQuorum makes sure to
    // preserve this.
    pr.RecentActive = true

    // Conservatively set the pendingConfIndex to the last index in the
    // log. There may or may not be a pending config change, but it's
    // safe to delay any future proposals until we commit all our
    // pending log entries, and scanning the entire tail of the log
    // could be expensive.
    r.pendingConfIndex = r.raftLog.lastIndex()

    emptyEnt := pb.Entry{Data: nil}
    if !r.appendEntry(emptyEnt) {
        // This won't happen because we just called reset() above.
        r.logger.Panic("empty entry was dropped")
    }
    // The payloadSize of an empty entry is 0 (see TestPayloadSizeOfEmptyEntry),
    // so the preceding log append does not count against the uncommitted log
    // quota of the new leader. In other words, after the call to appendEntry,
    // r.uncommittedSize is still 0.
    r.logger.Infof("%x became leader at term %d", r.id, r.Term)
}
```

这几个becomeXxx的方法主要都做了下面几件事：

1. 修改r.step
2. 修改r.Term
3. 修改r.tick
4. 修改r.lead或r.Vote
5. 修改r.state
6. 打log

除此之外becomeLeader做了一些处理Log Replication相关的内容

### rawnode、node、raft

前面我们主要讲了raft，下面我们稍微讲讲这三者的流程控制

node.go

```go
type node struct {
    ...
    tickc chan struct{}
    ...
}

type Node interface {
    ...
    Tick()
    ...
}

func (n *node) Tick() {
    select {
    case n.tickc <- struct{}{}:
    case <-n.done:
    default:
        n.rn.raft.logger.Warningf("%x A tick missed to fire. Node blocks too long!", n.rn.raft.id)
    }
}

func (n *node) run() {
    ...
    for {
        ...
        select {
        ...
        case <-n.tickc:
            n.rn.Tick()
        ...
        }
    }
    ...
}
```

rawnode.go

```go
func (rn *RawNode) Tick() {
    rn.raft.tick()
}
```

某用例
```go
func (n *node) start() {
    ...
    ticker := time.NewTicker(5 * time.Millisecond).C
    go func() {
        for {
            select {
            case <-ticker:
                n.Tick()
            ...
            }
        }
    }()
}
```

1. raft提供tick()方法
2. rawnode提供Tick()方法调用raft的tick()方法
3. node中通过循环select获取tickc管道的内容来掉用rawnode的Tick()方法
4. node提供Tick()方法想tickc管道输入内容，用来驱动第3步
5. 某用例中创建了一个5ms为间隔的ticker，并在ticker中调用node提供的Tick()方法

综上

* 我们定义一个逻辑时钟，在某用例里这个逻辑时钟间隔为5ms
* 逻辑时钟每跳动一下，我们调用一次node的Tick()方法
* 每调用一次node的Tick()方法，便向node的tickc管道中输入一个数据
* 每向node的tickc管道中输入一次数据，我们在node中调用一次rawnode的Tick()
* rawnode的Tick()方法仅仅是raft的tick()方法的简单封装
* 就相当于调用一次raft的tick()方法
* 而根据raft当前的角色不同，这个tick()方法实际上调用的是raft的tickElection或tickHeartbeat

### r.tick

我们上面提到了raft的tick实际上调用的是tickElection或tickHeartbeat

这两个方法利用到了`heartbeatElapsed`和`electionElapsed`这两个变量

这两个变量用来和下面三个变量比较，来处理几个核心超时概念

```go
heartbeatTimeout
electionTimeout
randomizedElectionTimeout
```

到这里我们应该明白raft中定义的上述三个超时，是逻辑超时  
比如我们定义了`heartbeatTimeout`为`10`，而我们在某用例中定义了一个逻辑时钟为`5ms`  
那么我们在某用例中实际的心跳超时时间为`5ms * 10 = 50ms`

那么我们是如何使用这几个超时时间的呢

tickElection
```go
// tickElection is run by followers and candidates after r.electionTimeout.
func (r *raft) tickElection() {
    r.electionElapsed++

    if r.promotable() && r.pastElectionTimeout() {
        r.electionElapsed = 0
        if err := r.Step(pb.Message{From: r.id, Type: pb.MsgHup}); err != nil {
            r.logger.Debugf("error occurred during election: %v", err)
        }
    }
}

// pastElectionTimeout returns true if r.electionElapsed is greater
// than or equal to the randomized election timeout in
// [electiontimeout, 2 * electiontimeout - 1].
func (r *raft) pastElectionTimeout() bool {
    return r.electionElapsed >= r.randomizedElectionTimeout
}
```

在tickElection中

1. 对electionElapsed进行计数，代表已经经历的逻辑时钟数
2. 用electionElapsed和randomizedElectionTimeout比较
3. 如果超时就调用Step，核心参数pb.MsgHup

tickHeartbeat
```go
// tickHeartbeat is run by leaders to send a MsgBeat after r.heartbeatTimeout.
func (r *raft) tickHeartbeat() {
	r.heartbeatElapsed++
	r.electionElapsed++

	if r.electionElapsed >= r.electionTimeout {
		r.electionElapsed = 0
		if r.checkQuorum {
			if err := r.Step(pb.Message{From: r.id, Type: pb.MsgCheckQuorum}); err != nil {
				r.logger.Debugf("error occurred during checking sending heartbeat: %v", err)
			}
		}
		// If current leader cannot transfer leadership in electionTimeout, it becomes leader again.
		if r.state == StateLeader && r.leadTransferee != None {
			r.abortLeaderTransfer()
		}
	}

	if r.state != StateLeader {
		return
	}

	if r.heartbeatElapsed >= r.heartbeatTimeout {
		r.heartbeatElapsed = 0
		if err := r.Step(pb.Message{From: r.id, Type: pb.MsgBeat}); err != nil {
			r.logger.Debugf("error occurred during checking sending heartbeat: %v", err)
		}
	}
}
```

在tickHeartbeat中

1. 对heartbeatElapsed和electionElapsed都进行计数
2. 判断electionTimeout，若超时就调用Step，核心参数pb.MsgCheckQuorum
    * 这一步主要用来判断leader的活跃程度，暂时不展开说
3. 如果上一步导致leader身份改变，则返回
4. 判断heartbeatTimeout，弱超时就调用Step，核心参数pb.MsgBeat

注：

* 我们可以看到，heartbeatElapsed仅在tickHeartbeat中用到，用来控制心跳请求
* 而两个tick方法中都用到了electionElapsed，但比较对象不同
* tickElection中比较的是randomizedElectionTimeout，是随机的，用来各个node错峰判断是否开启新的选举周期
* tickHeartbeat中比较的是基准electionTimeout，是固定的，用来判断leader的活跃程度

### r.Step

上面的tick方法本身主要是用来控制核心超时流程的  
主要的核心业务都调用了r.Step方法

```go
func (r *raft) Step(m pb.Message) error {
    // Handle the message term, which may result in our stepping down to a follower.
    switch {
        ...
    }

    switch m.Type {
    case pb.MsgHup:
        if r.preVote {
            r.hup(campaignPreElection)
        } else {
            r.hup(campaignElection)
        }
    ...
    case pb.MsgVote, pb.MsgPreVote:
        ...
    default:
        err := r.step(r, m)
        if err != nil {
            return err
        }
    }
    return nil
}
```

1. Step方法先判断Term相关信息，暂时不展开说
2. 接下来判断Msg的类型，不同的类型对应不同的操作，默认使用r.step方法进行处理

另外，我们可以看到前面tickElection中调用Step时传递的核心参数pb.MsgHup在这里被处理

### r.step

前面我们知道r.step方法主要是stepFollower、stepCandidate、stepLeader三个方法

对这三个函数的详细内容暂时不展开说

仅看tickHeartbeat中调用Step时传递的核心参数pb.MsgCheckQuorum和pb.MsgBeat在这里都有处理

```go
func stepLeader(r *raft, m pb.Message) error {
    // These message types do not require any progress for m.From.
    switch m.Type {
    case pb.MsgBeat:
        r.bcastHeartbeat()
        return nil
    case pb.MsgCheckQuorum:
        if !r.prs.QuorumActive() {
            r.logger.Warningf("%x stepped down to follower since quorum is not active", r.id)
            r.becomeFollower(r.Term, None)
        }
        // Mark everyone (but ourselves) as inactive in preparation for the next
        // CheckQuorum.
        r.prs.Visit(func(id uint64, pr *tracker.Progress) {
            if id != r.id {
                pr.RecentActive = false
            }
        })
        return nil
    ...
    }
    return nil
}
```

### 小结

至此，raft算法中由electionTimeout控制的选举周期和由heartbeatTimeout控制的心跳周期的控制流程大体讲完

## 我的raftkv

我的raftkv的实现这里就不细说了，大体上也完成了对上述两个周期的实现

但从实现逻辑上来说

* 我的raftkv使用了timer来计时
* 并使用reset和stop对计时器进行控制，并没有tickc的时间精度

raftkv的实现有一个最大的问题就是目前每次处理任务都得放在timer触发或者从其他地方获得请求的时候处理  
而etcd的raft是用一个逻辑时钟来记录时间流逝，可以在每一逻辑tick进行所需的工作处理而不需要等待来自别人的触发或者超时触发

## 学到了什么

* 如何实现一个逻辑时钟以及用它处理实时任务
* raft算法两个周期的基本实现方法和流程控制
