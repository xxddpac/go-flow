package kafka

import (
	"fmt"
	"github.com/xxddpac/async"
	"go-flow/conf"
	"go-flow/utils"
	"testing"
	"time"
)

func TestKafka(t *testing.T) {
	conf.Init("../config.toml")
	config := &Config{
		Brokers: conf.CoreConf.Kafka.Brokers,
		Topic:   conf.CoreConf.Kafka.Topic,
		P:       async.New(),
		Ctx:     utils.Ctx,
	}
	if err = Init(config); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			config.Q([]byte(fmt.Sprintf(`"msg":"test","timestamp":%s`, time.Now().Format(utils.TimeLayout))))
			time.Sleep(10 * time.Millisecond)
		}
	}()
	<-time.After(5 * time.Second)
	utils.Cancel()
	Close()
}
