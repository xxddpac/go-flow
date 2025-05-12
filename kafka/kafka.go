package kafka

import (
	"github.com/Shopify/sarama"
	"time"
)

var (
	producer sarama.AsyncProducer
	err      error
	Queue    = make(chan []byte, 10000)
	Kc       *Config
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
	Kc = config
	go func() {
		for {
			select {
			case msg := <-producer.Successes():
				//value, _ := msg.Value.Encode()
				//Kc.P.Logger.Printf("Send msg to kafka success, topic:%s, partition:%d, offset:%d,msg:%s\n", msg.Topic, msg.Partition, msg.Offset, string(value))
				_ = msg
			case err = <-producer.Errors():
				Kc.P.Logger.Printf("Failed to send message: %s", err.Error())
			case <-Kc.Ctx.Done():
				return
			}
		}
	}()
	go func() {
		for {
			select {
			case msg := <-Queue:
				Kc.send(msg)
			case <-Kc.Ctx.Done():
				return
			}
		}
	}()
	return nil
}

func Close() {
	producer.AsyncClose()
}

func (c *Config) Q(msg []byte) {
	select {
	case Queue <- msg:
	default:
		c.P.Logger.Printf("Kafka queue is full, dropping message: %s", string(msg))
	}
}

func (c *Config) send(msg []byte) {
	producer.Input() <- &sarama.ProducerMessage{Topic: c.Topic, Value: sarama.StringEncoder(msg)}
}
