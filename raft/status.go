package raft

import (
	"fmt"
)

func (np *node) PrintStatus() {
	fmt.Println(*np)
}
