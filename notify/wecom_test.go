package notify

import (
	"go-flow/conf"
	"go-flow/utils"
	"testing"
	"time"
)

func TestWeCom(t *testing.T) {
	conf.Init("../config.toml")
	var (
		wc  = &WeCom{}
		bws = make([]Bandwidth, 0, 10)
		fqs = make([]Frequency, 0, 10)
	)
	bws = append(bws,
		Bandwidth{IP: "10.188.61.20", Bandwidth: "2GB"},
		Bandwidth{IP: "10.188.61.21", Bandwidth: "700MB"},
		Bandwidth{IP: "10.188.61.22", Bandwidth: "500MB"},
		Bandwidth{IP: "10.188.61.29", Bandwidth: "400MB"},
		Bandwidth{IP: "221.133.2.20", Bandwidth: "400MB"})
	fqs = append(fqs,
		Frequency{Desc: "检测到源IP 10.188.61.20 在最近1分钟内尝试连接 10000 个不同目标 IP，疑似自动化探测等异常行为"},
		Frequency{Desc: "检测到源IP 10.188.61.21 在最近1分钟内尝试连接 10000 个不同目标 IP，疑似自动化探测等异常行为"},
		Frequency{Desc: "检测到源IP 10.188.61.22 在最近1分钟内尝试连接 10000 个不同目标 IP，疑似自动化探测等异常行为"},
		Frequency{Desc: "检测到目标IP 221.133.2.19 在最近1分钟内接收来自 10000 个不同源IP访问请求，疑似分布式拒绝服务等异常行为"},
		Frequency{Desc: "检测到目标IP 221.133.2.20 在最近1分钟内接收来自 10000 个不同源IP访问请求，疑似分布式拒绝服务等异常行为"},
		Frequency{Desc: "检测到目标IP 221.133.2.21 在最近1分钟内接收来自 10000 个不同源IP访问请求，疑似分布式拒绝服务等异常行为"},
	)
	err := wc.Send(DdosAlert{
		Title:      "大流量预警",
		Location:   "办公网",
		Timestamp:  time.Now().Format(utils.TimeLayout),
		BandwidthS: bws,
		TimeRange:  utils.GetTimeRangeString(5),
	})
	if err != nil {
		t.Fatal(err)
	}
	err = wc.Send(DdosAlert{
		Title:      "高频请求预警",
		Location:   "办公网",
		Timestamp:  time.Now().Format(utils.TimeLayout),
		TimeRange:  utils.GetTimeRangeString(1),
		FrequencyS: fqs})
	if err != nil {
		t.Fatal(err)
	}
}
