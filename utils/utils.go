package utils

import (
	"context"
	"fmt"
	"github.com/google/gopacket/pcap"
	"net"
	"strings"
	"time"
)

const (
	TimeLayout       = "2006-01-02 15:04:05"
	BroadcastAddress = "255.255.255.255"
)

var (
	Ctx    context.Context
	Cancel context.CancelFunc
)

func init() {
	Ctx, Cancel = context.WithCancel(context.Background())
}

var PortMapping = map[string]string{
	"20":    "FTP",
	"21":    "FTP",
	"22":    "SSH",
	"23":    "Telnet",
	"25":    "SMTP",
	"53":    "DNS",
	"69":    "TFTP",
	"80":    "HTTP",
	"110":   "POP3",
	"123":   "NTP",
	"135":   "MS-RPC",
	"139":   "NetBIOS-SSN",
	"143":   "IMAP",
	"161":   "SNMP",
	"162":   "SNMP",
	"179":   "BGP",
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
	"1723":  "PPTP",
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
	"10050": "Zabbix",
	"10051": "Zabbix",
}

func GetTimeRangeString(minutes int) string {
	now := time.Now()
	start := now.Add(-time.Duration(minutes * int(time.Minute)))
	return fmt.Sprintf("%s - %s",
		start.Format("2006-01-02 15:04:05"),
		now.Format("2006-01-02 15:04:05"))
}

func IsValidIP(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}
	if parsedIP.IsLoopback() || parsedIP.IsUnspecified() {
		return false
	}
	if parsedIP.IsMulticast() {
		return false
	}
	if ip == BroadcastAddress {
		return false
	}
	if strings.HasPrefix(ip, "169.254.") {
		return false
	}
	return true
}

func ListAvailableDevices() {
	devices, err := pcap.FindAllDevs()
	if err != nil {
		fmt.Printf("无法获取网卡列表: %v\n", err)
		return
	}
	if len(devices) == 0 {
		fmt.Println("未发现任何可用网卡。")
		return
	}
	for _, dev := range devices {
		fmt.Printf("- %s", dev.Name)
		if dev.Description != "" {
			fmt.Printf(" (%s)", dev.Description)
		}
		fmt.Println()
	}
}
