package conf

import (
	"fmt"
	"github.com/BurntSushi/toml"
	"go-flow/kafka"
	"go-flow/zlog"
	"os"
)

var (
	CoreConf *config
)

func Init(conf string) {
	_, err := toml.DecodeFile(conf, &CoreConf)
	if err != nil {
		fmt.Printf("Err %v", err)
		os.Exit(1)
	}
}

type config struct {
	Server Server
	Mail   Mail
	WeCom  WeCom
	Notify Notify
	Kafka  kafka.Config
	Log    zlog.Config
}

type Server struct {
	Port    int
	Workers int
	Size    int
	Rank    int
	Eth     string
}

type WeCom struct {
	Enable  bool
	WebHook string
}

type Mail struct {
	Enable   bool
	SmtpPort int
	SmtpHost string
	Username string
	Password string
	From     string
	To       string
	Cc       []string
}

type Notify struct {
	Enable             bool
	ThresholdValue     float64
	ThresholdUnit      string
	Whitelist          []string
	Location           string
	FrequencyThreshold int
}
