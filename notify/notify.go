package notify

import (
	"go-flow/conf"
	"go-flow/utils"
	"go-flow/zlog"
	"sync"
)

type Notify interface {
	Send(d DdosAlert) error
}

var (
	_ Notify = (*Mail)(nil)
	_ Notify = (*WeCom)(nil)
)

var (
	ns        = make([]Notify, 0, 10)
	ddosChan  = make(chan DdosAlert, 100)
	whitelist []string
)

func Init(wg *sync.WaitGroup) {
	defer wg.Done()
	whitelist = conf.CoreConf.Notify.Whitelist
	if conf.CoreConf.Mail.Enable {
		ns = append(ns, &Mail{})
	}
	if conf.CoreConf.WeCom.Enable {
		ns = append(ns, &WeCom{})
	}
	for {
		select {
		case <-utils.Ctx.Done():
			close(ddosChan)
			return
		case d := <-ddosChan:
			for _, n := range ns {
				if err := n.Send(d); err != nil {
					zlog.Errorf("Notify", "send notify error: %v", err)
				}
			}
		}
	}
}

type Bandwidth struct {
	IP        string
	Bandwidth string
}

type Frequency struct {
	IP    string
	Count int
	Desc  string
}

type DdosAlert struct {
	BandwidthS []Bandwidth
	FrequencyS []Frequency
	Title      string
	Timestamp  string
	Location   string
	TimeRange  string
}

func Push(d DdosAlert) {
	select {
	case ddosChan <- d:
	default:
		zlog.Warnf("Notify", "notify queue is full, dropping message")
	}
}

func IsWhiteIp(ip string) bool {
	for index := range whitelist {
		if whitelist[index] == ip {
			return true
		}
	}
	return false
}
