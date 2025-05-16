package utils

import (
	"fmt"
	"testing"
)

func TestIsValidIP(t *testing.T) {
	fmt.Println(IsValidIP("1.2.2.1"))
}

func TestGetCpuUsage(t *testing.T) {
	fmt.Println(GetCpuUsage())
}

func TestGetLocalIp(t *testing.T) {
	fmt.Println(GetLocalIp())
}

func TestGetMemUsage(t *testing.T) {
	fmt.Println(GetMemUsage())
}

func TestGetTimeRangeString(t *testing.T) {
	fmt.Println(GetTimeRangeString(1))
}

func TestListAvailableDevices(t *testing.T) {
	ListAvailableDevices()
}
