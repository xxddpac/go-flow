package notify

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"go-flow/conf"
	"gopkg.in/gomail.v2"
	"text/template"
)

const MailTemplate = `
<!DOCTYPE html>
<html lang="zh">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}}</title>
</head>
<body style="font-family: Arial, sans-serif; color: #333; margin: 0; padding: 20px; background-color: #f5f5f5;">
    <div style="max-width: 600px; margin: 0 auto; background-color: #ffffff; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); padding: 20px;">
        <h1 style="color: #d32f2f; font-size: 24px; margin: 0 0 20px;">{{.Title}}</h1>
        <div style="font-size: 16px; line-height: 1.6;">
            {{if .Timestamp}}
            <p style="margin: 0 0 10px;">告警时间：{{.Timestamp}}</p>
            {{end}}
            {{if .Location}}
            <p style="margin: 0 0 10px;">所属位置：{{.Location}}</p>
            {{end}}
            {{if .TimeRange}}
            <p style="margin: 0 0 20px;">滑动窗口：{{.TimeRange}}</p>
            {{end}}
            {{if .BandwidthS}}
            <table style="width: 100%; border-collapse: collapse; margin-top: 10px;">
                <thead>
                    <tr style="background-color: #f0f0f0;">
                        <th style="padding: 10px; text-align: left; border: 1px solid #ddd;">#</th>
                        <th style="padding: 10px; text-align: left; border: 1px solid #ddd;">异常IP</th>
                        <th style="padding: 10px; text-align: left; border: 1px solid #ddd;">使用流量</th>
                    </tr>
                </thead>
                <tbody>
                    {{range $index, $alert := .BandwidthS}}
                    <tr>
                        <td style="padding: 10px; border: 1px solid #ddd;">{{add $index 1}}</td>
                        <td style="padding: 10px; border: 1px solid #ddd;">{{$alert.IP}}</td>
                        <td style="padding: 10px; border: 1px solid #ddd;">{{$alert.Bandwidth}}</td>
                    </tr>
                    {{end}}
                </tbody>
            </table>
            {{end}}
            {{if .FrequencyS}}
            <table style="width: 100%; border-collapse: collapse; margin-top: 10px;">
                <thead>
                    <tr style="background-color: #f0f0f0;">
                        <th style="padding: 10px; text-align: left; border: 1px solid #ddd;">#</th>
                        <th style="padding: 10px; text-align: left; border: 1px solid #ddd;">描述</th>
                    </tr>
                </thead>
                <tbody>
                    {{range $index, $alert := .FrequencyS}}
                    <tr>
                        <td style="padding: 10px; border: 1px solid #ddd;">{{add $index 1}}</td>
                        <td style="padding: 10px; border: 1px solid #ddd;">{{$alert.Desc}}</td>
                    </tr>
                    {{end}}
                </tbody>
            </table>
            {{end}}
        </div>
    </div>
</body>
</html>`

type Mail struct {
}

var (
	smtpPort int
	smtpHost string
	username string
	password string
	from     string
	to       string
	cc       []string
)

func (m *Mail) Send(d DdosAlert) error {
	var (
		err  error
		tmpl *template.Template
	)
	tmpl, err = template.New("html_alert").Funcs(funcMap).Parse(MailTemplate)
	if err != nil {
		return err
	}
	var msg bytes.Buffer
	if err = tmpl.Execute(&msg, d); err != nil {
		return err
	}
	smtpPort = conf.CoreConf.Mail.SmtpPort
	smtpHost = conf.CoreConf.Mail.SmtpHost
	username = conf.CoreConf.Mail.Username
	password = conf.CoreConf.Mail.Password
	from = conf.CoreConf.Mail.From
	to = conf.CoreConf.Mail.To
	cc = conf.CoreConf.Mail.Cc
	mail := gomail.NewMessage()
	mail.SetHeader("From", from)
	mail.SetHeader("To", to)
	mail.SetHeader("Cc", cc...)
	mail.SetHeader("Subject", d.Title)
	mail.SetBody("text/html", msg.String())
	dialer := gomail.NewDialer(smtpHost, smtpPort, username, password)
	dialer.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	if err = dialer.DialAndSend(mail); err != nil {
		return fmt.Errorf("failed to send mail: %w", err)
	}
	return nil
}
