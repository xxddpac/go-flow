package main

import (
	"context"
	"sync"
)

const (
	timeLayout = "2006-01-02 15:04:05"
	port       = 31415
)

var (
	ctx      context.Context
	cancel   context.CancelFunc
	syncPool = sync.Pool{New: func() interface{} { return &Traffic{} }}
)

func init() {
	ctx, cancel = context.WithCancel(context.Background())
}
