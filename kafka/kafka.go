package kafka

import (
	"github.com/Shopify/sarama"
	"go-flow/utils"
	"go-flow/zlog"
	"time"
)

var (
	producer sarama.AsyncProducer
	err      error
	Queue    chan []byte
)

func Init(config *Config) error {
	kafkaConfig := sarama.NewConfig()
	kafkaConfig.Producer.Timeout = 5 * time.Second
	kafkaConfig.Producer.Return.Successes = true
	kafkaConfig.Producer.MaxMessageBytes = 1024 * 1024 * 10
	kafkaConfig.Producer.Compression = sarama.CompressionSnappy
	producer, err = sarama.NewAsyncProducer(config.Brokers, kafkaConfig)
	if err != nil {
		return err
	}
	Queue = make(chan []byte, config.Size)
	go func() {
		for {
			select {
			case msg := <-producer.Successes():
				//value, _ := msg.Value.Encode()
				//zlog.Infof("Kafka", "topic: %s, partition: %d, offset: %d, value: %s", msg.Topic, msg.Partition, msg.Offset, string(value))
				_ = msg
			case err = <-producer.Errors():
				zlog.Errorf("Kafka", "Failed to send message: %s", err.Error())
			case <-utils.Ctx.Done():
				return
			}
		}
	}()
	go func() {
		for {
			select {
			case msg := <-Queue:
				producer.Input() <- &sarama.ProducerMessage{Topic: config.Topic, Value: sarama.StringEncoder(msg)}
			case <-utils.Ctx.Done():
				return
			}
		}
	}()
	return nil
}

func Push(msg []byte) {
	select {
	case Queue <- msg:
	default:
		zlog.Warnf("Kafka", "Kafka queue is full, dropping message")
	}
}

func Close() {
	if producer == nil {
		return
	}
	producer.AsyncClose()
}
