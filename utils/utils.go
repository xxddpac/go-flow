package utils

import (
	"context"
	"fmt"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"net"
	"strings"
	"time"
)

const (
	TimeLayout       = "2006-01-02 15:04:05"
	BroadcastAddress = "255.255.255.255"
)

var (
	Ctx         context.Context
	Cancel      context.CancelFunc
	LocalIpAddr string
)

func init() {
	Ctx, Cancel = context.WithCancel(context.Background())
	LocalIpAddr = GetLocalIp()
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

func GetLocalIp() string {
	var (
		err         error
		localIpAddr string
		ifs         []net.Interface
		as          []net.Addr
	)
	if localIpAddr != "" {
		return localIpAddr
	}
	ifs, err = net.Interfaces()
	if err != nil {
		return "Unknown"
	}
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 {
			continue
		}
		if i.Flags&net.FlagLoopback != 0 {
			continue
		}
		as, err = i.Addrs()
		if err != nil {
			return "Unknown"
		}
		for _, addr := range as {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()

			if ip == nil {
				continue
			}
			localIpAddr = ip.String()
			return localIpAddr
		}
	}
	return "Unknown"
}

func GetCpuUsage() string {
	percent, _ := cpu.Percent(time.Second, false)
	if len(percent) > 0 {
		return fmt.Sprintf("%.2f%%", percent[0])
	}
	return "Unknown"
}

func GetMemUsage() string {
	v, _ := mem.VirtualMemory()
	if v != nil {
		return fmt.Sprintf("%.2f%%", v.UsedPercent)
	}
	return "Unknown"
}
