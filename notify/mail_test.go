package notify

import (
	"go-flow/conf"
	"go-flow/utils"
	"testing"
	"time"
)

func TestMail(t *testing.T) {
	conf.Init("../config.toml")
	mail := &Mail{}
	var alerts = make([]Ddos, 0, 10)
	alerts = append(alerts, Ddos{IP: "1.1.1.1", Bandwidth: "1GB"}, Ddos{IP: "2.2.2.2", Bandwidth: "200MB"},
		Ddos{IP: "3.3.3.3", Bandwidth: "300MB"}, Ddos{IP: "4.4.4.4", Bandwidth: "400MB"}, Ddos{IP: "5.5.5.5", Bandwidth: "500MB"})
	if err := mail.Send(DdosAlert{
		Location:  "Office",
		Timestamp: time.Now().Format(utils.TimeLayout),
		Alerts:    alerts,
		TimeRange: utils.GetTimeRangeString(5),
	}); err != nil {
		t.Fatal(err)
	}
}
