package kafka

type Config struct {
	Enable  bool
	Brokers []string
	Topic   string
	Size    int
}
