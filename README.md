# raftkv
raft server demo in go

vi main.go

```go
func main() {
	fmt.Println("hello raftkv")

	//startOneNode()
	startThreeNodes()
}
```

startOneNode或者startThreeNodes

go run main.go

日志在node目录中

本项目仅用来展示raft算法的选举周期和心跳周期的大体逻辑

其他文档查看：

etcd-raft代码分析：doc/my_raft_etcd_raft.md

raft算法简介：doc/raft.md

因为自己手搓完raft基本算法之后就去看etcd的raft代码了，所以项目虽然叫raftkv，但实际上kv部分并没有实现，Log Replication也并没有实现

暂时就这些

在doc目录中增加raft算法原论文和论文翻译

在线链接：

原论文：https://raft.github.io/raft.pdf

翻译：https://object.redisant.com/doc/raft中译版-2023年4月23日.pdf
