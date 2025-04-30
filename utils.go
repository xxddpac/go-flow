package main

import (
	"context"
	"sync"
)

const (
	timeLayout = "2006-01-02 15:04:05"
	port       = 31415
)

var (
	ctx      context.Context
	cancel   context.CancelFunc
	syncPool = sync.Pool{New: func() interface{} { return &Traffic{} }}
)

func init() {
	ctx, cancel = context.WithCancel(context.Background())
}

var portMapping = map[string]string{
	"20":    "FTP",
	"21":    "FTP",
	"22":    "SSH",
	"23":    "Telnet",
	"25":    "SMTP",
	"53":    "DNS",
	"69":    "TFTP",
	"80":    "HTTP",
	"110":   "POP3",
	"135":   "MS-RPC",
	"139":   "NetBIOS-SSN",
	"143":   "IMAP",
	"169":   "SNMP",
	"389":   "LDAP",
	"443":   "HTTPS",
	"445":   "SMB",
	"465":   "SMTPS",
	"514":   "Syslog",
	"587":   "SMTP-Submission",
	"636":   "LDAPS",
	"993":   "IMAPS",
	"995":   "POP3S",
	"1433":  "SQL-Server",
	"1521":  "Oracle",
	"2379":  "Etcd",
	"2181":  "ZooKeeper",
	"27017": "MongoDB",
	"3306":  "MySQL",
	"3389":  "RDP",
	"5432":  "PostgreSQL",
	"5601":  "Kibana",
	"5672":  "RabbitMQ",
	"6379":  "Redis",
	"8500":  "Consul",
	"9092":  "Kafka",
	"9200":  "Elasticsearch",
}
