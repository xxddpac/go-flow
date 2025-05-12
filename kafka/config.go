package kafka

import (
	"context"
	"github.com/xxddpac/async"
)

type Config struct {
	Brokers []string
	Topic   string
	P       *async.WorkerPool
	Ctx     context.Context
}
