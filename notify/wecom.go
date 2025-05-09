package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go-flow/conf"
	"go-flow/http"
	"text/template"
)

const WcComTemplate = "【GO-FLOW】 `**{{if .Title}}{{.Title}}{{end}}**`\n" +
	"{{if .Timestamp}}>告警时间：{{.Timestamp}}\n{{end}}" +
	"{{if .Location}}>所属位置：{{.Location}}\n{{end}}" +
	"{{if .TimeRange}}>滑动窗口：{{.TimeRange}}\n{{end}}" +
	"**== 告警详情 ==**\n" +
	"{{if .BandwidthS}}" +
	"{{range $index, $alert := .BandwidthS}}" +
	"{{add $index 1}} 异常IP <font color=\"warning\">{{$alert.IP}}</font>, 使用流量 <font color=\"warning\">{{$alert.Bandwidth}}</font>\n" +
	"{{end}}" +
	"{{end}}" +
	"{{if .FrequencyS}}" +
	"{{range $index, $alert := .FrequencyS}}" +
	"{{add $index 1}} <font color=\"warning\">{{$alert.Desc}}</font>\n" +
	"{{end}}" +
	"{{end}}"

var (
	request = http.NewClient("")
	url     string
	funcMap = template.FuncMap{
		"add": func(a, b int) int {
			return a + b
		},
	}
)

type Message struct {
	MsgType  string   `json:"msgtype"`
	Markdown Markdown `json:"markdown"`
}

type Markdown struct {
	Content string `json:"content"`
}

type WeCom struct {
}

type WeComResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

func (w *WeCom) Send(d DdosAlert) error {
	var (
		err  error
		b    []byte
		tmpl *template.Template
	)
	url = conf.CoreConf.WeCom.WebHook
	tmpl, err = template.New("alert").Funcs(funcMap).Parse(WcComTemplate)
	if err != nil {
		return err
	}
	var msg bytes.Buffer
	if err = tmpl.Execute(&msg, d); err != nil {
		return err
	}
	b, err = request.Post(url, &Message{MsgType: "markdown", Markdown: Markdown{msg.String()}})
	if err != nil {
		return err
	}
	var weComResponse WeComResponse
	if err = json.Unmarshal(b, &weComResponse); err != nil {
		return err
	}
	if weComResponse.ErrCode != 0 {
		return fmt.Errorf("wecom send error, code: %d, msg: %s", weComResponse.ErrCode, weComResponse.ErrMsg)
	}
	return nil
}
