package raft

import (
	"fmt"
	"io"
	"log"
	"os"
)

const (
	DefaultFileMode = 0666
	DefaultDirMode  = 0777
)

type logger struct {
	curDir   string
	fullDir  string
	fullPath string
	fd       io.Writer
	prefix   string
	logger   *log.Logger
	err      error
}

func (rl *logger) setCurDir() {
	if rl.err != nil {
		return
	}
	rl.curDir, rl.err = os.Getwd()
}

func (rl *logger) makeDirWithNode(dir string) {
	if rl.err != nil {
		return
	}
	rl.fullDir = rl.curDir + dir
	rl.err = os.MkdirAll(rl.fullDir, DefaultDirMode)
}

func (rl *logger) openFileCreateAppend(file string) {
	if rl.err != nil {
		return
	}
	rl.fullPath = rl.fullDir + file
	rl.fd, rl.err = os.OpenFile(rl.fullPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, DefaultFileMode)
}

func (rl *logger) newLogger(prefix string) {
	if rl.err != nil {
		return
	}
	rl.prefix = prefix
	rl.logger = log.New(rl.fd, prefix, log.Lmicroseconds)
}

func NewLogger(n uint64) (*logger, error) {
	rl := &logger{}

	rl.setCurDir()
	nodeDir := fmt.Sprintf("/node/n_%v/", n)
	rl.makeDirWithNode(nodeDir)
	nodeFile := fmt.Sprintf("n_%v.log", n)
	rl.openFileCreateAppend(nodeFile)
	prefix := fmt.Sprintf("[n_%v] ", n)
	rl.newLogger(prefix)

	if rl.err != nil {
		return nil, rl.err
	}
	return rl, nil
}

func (np *node) logf(f string, v ...any) {
	np.base.logger.logger.Printf(f, v...)
}

func (np *node) logln(v ...any) {
	np.base.logger.logger.Println(v...)
}
