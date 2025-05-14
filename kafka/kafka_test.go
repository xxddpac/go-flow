package kafka

import (
	"fmt"
	"go-flow/utils"
	"go-flow/zlog"
	"testing"
	"time"
)

var (
	fakeKafkaConfig = &Config{
		Enable:  true,
		Brokers: []string{"localhost:9092"},
		Topic:   "go-flow",
		Size:    100,
	}
	fakeLogConfig = &zlog.Config{
		LogLevel:   "debug",
		Compress:   true,
		MaxSize:    10,
		MaxBackups: 5,
		MaxAge:     30,
		Format:     "json",
	}
)

func TestKafka(t *testing.T) {
	zlog.Init(zlog.NewZLog(fakeLogConfig))
	if err = Init(fakeKafkaConfig); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			Push([]byte(fmt.Sprintf("test %s", time.Now().Format(utils.TimeLayout))))
			time.Sleep(10 * time.Millisecond)
		}
	}()
	<-time.After(5 * time.Second)
	utils.Cancel()
	Close()
}
