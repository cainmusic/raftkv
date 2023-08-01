package raft

import (
	"time"
)

func timerStop(t *time.Timer) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
}

func timerReset(t *time.Timer, d time.Duration) {
	timerStop(t)
	t.Reset(d)
}
