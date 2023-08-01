package main

import (
	"fmt"

	"github.com/cainmusic/raftkv/raft"
)

func main() {
	fmt.Println("hello raftkv")

	startOneNode()
	//startThreeNodes()
}

func startOneNode() {
	node9, err := raft.New(9, []uint64{9})
	if err != nil {
		fmt.Println(err)
		return
	}
	go node9.Start()

	for {
		fmt.Scanln()
		node9.PrintStatus()
	}
}

func startThreeNodes() {
	nodeMap := make(map[uint64]*raft.Node, 3)

	node1, err := raft.New(1, []uint64{1, 2, 3})
	nodeMap[1] = node1

	node2, err := raft.New(2, []uint64{1, 2, 3})
	nodeMap[2] = node2

	node3, err := raft.New(3, []uint64{1, 2, 3})
	nodeMap[3] = node3

	if err != nil {
		fmt.Println(err)
		return
	}

	sendMsg := func(from, to uint64, msg raft.Msg) raft.Msg {
		fmt.Println(from, to, msg)
		nodeTIn, nodeTOut := nodeMap[to].GetMsgChan()
		nodeTIn <- msg
		res := <-nodeTOut
		fmt.Println(res)
		return res
	}

	node1.SetFuncSendMsg(sendMsg)
	node2.SetFuncSendMsg(sendMsg)
	node3.SetFuncSendMsg(sendMsg)

	go node1.Start()
	go node2.Start()
	go node3.Start()

	for {
		fmt.Scanln()
		node1.PrintStatus()
		node2.PrintStatus()
		node3.PrintStatus()
	}
}
