# Raft

## Understandable Distributed Consensus
## 可理解的分布式共识

Raft is a protocol for implementing distributed consensus  
Raft是一种实现分布式共识的协议

1. 一个节点可以有三种状态中的一个：
    * Leader
    * Candidate
    * Follower
2. 所有节点开始于follower状态
3. 当`followers`没有找到leader的时候，他们可以成为candidate
4. candidate会向其他节点请求投票
5. 如果candidate获得绝大多数节点的投票，就会成为leader

上面这个过程被称为`Leader Election`

接下来系统里的所有修改都要经过这个leader

1. 每一个修改请求都会产生一个`entry`，被加入leader的`node's log`
2. 这个`entry`目前还处于`uncommitted`状态，所以它不会修改节点的值
3. 为了`commit`这个`entry`，leader节点会把这个`entry`复制传递到follower节点
4. leader等待大部分节点写入了这个条目
5. leader对这个修改进行`commit`，leader的值被修改了
6. leader通知`followers`，这个条目`commit`了
7. `follows`对这个值进行了修改，整个系统达成了共识

上面这个过程被称为`Log Replication`

注：

1. `entry` 条目
2. `node's log` 节点日志
3. `uncommitted` 未提交的，`commit` 提交

### Leader Election

Raft中有两个timeout设置控制着election

1. election timeout
    * follower在变成candidate之前等待的时间
    * 在150ms-300ms之内随机（相当于，时间越短越容易成为candidate）
    * `election timeout`结束后，follower成为了candidate并开始了`election term`
    * candidate先给自己投票，再向其他节点发送`Request Vote`信息
    * 如果接收请求的节点在这一轮`election term`中还未投票，则它投给candidate，并重置自己的`election timeout`
    * 一旦candidate获得了大部分投票，它便会成为leader
2. heartbeat timeout（实际上是interval）
    * leader会开始给followers发送`append entries`信息
    * 这些消息按`heartbeat timeout`设定的`interval`发送
    * followers会响应这些`append entries`消息
3. 联合作用
    * 当前`election term`会持续到一个follower没有收到`heartbeat`请求并达到`election timeout`成为candidate

注：

1. `election timeout` 选举超时
2. `election term` 选举周期
    * 从一个follower没有收到`heartbeat`请求并达到`election timeout`成为candidate开始
    * 到选举完成之后，下一个candidate出现（条件同上）
3. `Request Vote` 请求投票
4. `append entries` 追加条目
    * `entry`是上一小节提到的由修改请求产生的条目
    * leader将这个`entry`复制发送给followers的过程就是`append entries`请求
5. `heartbeat timeout` 心跳超时，`heartbeat` 心跳，`interval` 时间间隔
    * `heartbeat timeout`实质上是一个`interval`

尝试停止一个leader并开始一个新的`election term`

当前我们有ABC三个node，leader是A，以`heartbeat timeout`为`interval`向BC发送请求维持服务状态  
接下来我们停止A，开始一个新的`election term`
1. 停止A
2. BC的`election timeout`没有被`heartbeat`重置
3. 由于BC的`election timeout`不同，假设B先达到了
4. B成为candidate，增加一个`election term`，给自己投票，并向AC进行`Request Vote`
5. A已经停止了，没有响应
6. C从B的请求中发现新的`election term`，重置自己的`election timeout`，并且在当前`election term`没有投票于是投票给A
7. B获得超过一半的投票，成为leader，并告知AC
8. C把自己的leader修改为B

需要大部分（超过一半）的投票数才能成为leader，保证了每个周期里只会产生一个leader

如果两个或多个node同时成为candidate，一个分裂的投票结果会产生

当前我们有ABCD四个node，没有leader，各个node开始`election timeout`计时准备开启一个新的`election term`
1. A和B几乎同时达到`election timeout`，他们都做了下面的事
    * 成为candidate
    * 增加一个`election term`
    * 给自己投票
    * 向其他三个node进行`Request Vote`
2. A的请求早于B的请求到达C，而B的请求早于A的请求到达D
3. C投票给了A，D投票给了B
4. A和B都没有获得超过一半的投票，本轮投票结束，没有candidate成为leader

这一轮没有产生新的leader，各个node开始新的`election timeout`计时准备开启一个新的`election term`
1. 这一次ABCD几乎同时达到`election timeout`，他们仍然做了上面提到的几件事
2. 这一次由于每个node都投票给了自己，于是没有node可以投票给的node
3. ABCD都没有超过一半投票，本轮投票结束，没有candidate成为leader

在4个node的集群中，只有两种情况可以产生新的leader
1. 在收到其他candidate的请求前，只有一个candidate时，这个candidate会成为leader
2. 在有两个candidate向其他node发出请求时，其中一个candidate的请求到达其他followers更早，这个candidate会成为leader

candidate成为leader的关键
1. 更早达到`election timeout`
2. 与其他node更快的交流速度

### Log Replication

一旦leader选举出来了，我们就需要复制所有的修改到系统里的每个node

这个操作是通过在`heartbeat`时使用相同的`append entries`信息来完成的

让我们来模拟这一过程，我们目前有ABC三个node，A是leader

1. 一个用户向leader发送了一条修改请求，set x，这一请求被写入leader的`node's log`
2. 然后这个修改会在下一个`heartbeat`时发送给其他所有followers
3. 在大多数followers都承认（或者说知悉，英文acknowledge）这个修改时，这个`entry`被`commit`
4. 然后响应被发送给用户

上面的过程提到了`entry`被`commit`之后就响应给了用户  
后面还会有leader写入数据以及告知followers的过程同时进行

### 参考

其中有可视化和可交互的示例  
本文主要来源于参考1

1. http://thesecretlivesofdata.com/raft/
2. https://raft.github.io/
