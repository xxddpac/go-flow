package notify

import (
	"context"
	"github.com/xxddpac/async"
	"go-flow/conf"
)

type Notify interface {
	Send(d DdosAlert) error
}

var _ Notify = (*Mail)(nil)
var _ Notify = (*WeCom)(nil)

var (
	ns             = make([]Notify, 0, 10)
	ddosChan       = make(chan DdosAlert, 100)
	Base           *base
	isNotifyEnable bool
	whitelist      []string
)

type base struct {
	p *async.WorkerPool
}

func Init(ctx context.Context, p *async.WorkerPool) {
	defer p.Wg.Done()
	Base = &base{p: p}
	whitelist = conf.CoreConf.Notify.Whitelist
	isNotifyEnable = conf.CoreConf.Notify.Enable
	if conf.CoreConf.Mail.Enable {
		ns = append(ns, &Mail{})
	}
	if conf.CoreConf.WeCom.Enable {
		ns = append(ns, &WeCom{})
	}
	for {
		select {
		case <-ctx.Done():
			close(ddosChan)
			p.Logger.Printf("notify quit")
			return
		case d := <-ddosChan:
			if !isNotifyEnable {
				continue
			}
			for _, n := range ns {
				if err := n.Send(d); err != nil {
					p.Logger.Printf("notify send err: %v", err)
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

func (b *base) Queue(d DdosAlert) {
	select {
	case ddosChan <- d:
	default:
		b.p.Logger.Printf("notify channel full, drop %v", d)
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
