package flow

import (
	"container/heap"
	"embed"
	"encoding/json"
	"fmt"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	ji "github.com/json-iterator/go"
	"github.com/spf13/cast"
	"github.com/xxddpac/async"
	"go-flow/conf"
	"go-flow/kafka"
	"go-flow/notify"
	"go-flow/utils"
	"math"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

var syncPool = sync.Pool{New: func() interface{} { return &Traffic{} }}

const (
	highFrequency = "高频请求预警"
	highBandwidth = "大流量预警"
)

type Traffic struct {
	Timestamp time.Time `json:"timestamp"`
	SrcIP     string    `json:"src_ip"`
	DstIP     string    `json:"dest_ip"`
	DstPort   string    `json:"dest_port"`
	Protocol  string    `json:"protocol"`
	Bytes     float64   `json:"bandwidth"`
	Requests  int64     `json:"requests"`
	Unit      string    `json:"unit"`
}

type IPTraffic struct {
	IP       string  `json:"ip"`
	Bytes    float64 `json:"bytes"`
	Requests int64   `json:"requests"`
	Unit     string  `json:"unit"`
}

type IPTrafficResponse struct {
	Data []IPTraffic `json:"data"`
	Base
}

type PortTraffic struct {
	Protocol string  `json:"protocol"`
	DstPort  string  `json:"dest_port"`
	Bytes    float64 `json:"bytes"`
	Unit     string  `json:"unit"`
}

type TrendItem struct {
	Timestamp int64   `json:"timestamp"`
	Bytes     float64 `json:"bytes"`
	Requests  int64   `json:"requests"`
	Unit      string  `json:"unit"`
}

type Bucket struct {
	Timestamp int64
	Stats     map[string]Traffic
}

type Window struct {
	buckets       []*Bucket
	size          time.Duration
	bucketMu      sync.RWMutex
	cache         []Traffic
	ipCache       []IPTraffic
	trendCache    []TrendItem
	protoCache    ProtocolCache
	cacheMu       sync.RWMutex
	lastCalc      time.Time
	Rank          int
	portCache     []PortTraffic
	srcToDstStats map[string]map[string]bool
	dstToSrcStats map[string]map[string]bool
	freqStatsMu   sync.RWMutex
}

func NewWindow(size time.Duration, rank int) *Window {
	bucketCount := int(size / time.Minute)
	buckets := make([]*Bucket, bucketCount)
	for i := range buckets {
		buckets[i] = &Bucket{
			Timestamp: 0,
			Stats:     make(map[string]Traffic),
		}
	}
	return &Window{
		buckets:       buckets,
		size:          size,
		Rank:          rank,
		srcToDstStats: make(map[string]map[string]bool),
		dstToSrcStats: make(map[string]map[string]bool),
	}
}

type TrafficHeap []Traffic

func (h TrafficHeap) Len() int {
	return len(h)
}

func (h TrafficHeap) Less(i, j int) bool {
	return h[i].Bytes*getUnitFactor(h[i].Unit) < h[j].Bytes*getUnitFactor(h[j].Unit)
}

func (h TrafficHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *TrafficHeap) Push(x interface{}) {
	*h = append(*h, x.(Traffic))
}

func (h *TrafficHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

type IPTrafficHeap []IPTraffic

func (h IPTrafficHeap) Len() int { return len(h) }
func (h IPTrafficHeap) Less(i, j int) bool {
	return h[i].Bytes*getUnitFactor(h[i].Unit) < h[j].Bytes*getUnitFactor(h[j].Unit)
}
func (h IPTrafficHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *IPTrafficHeap) Push(x interface{}) {
	*h = append(*h, x.(IPTraffic))
}
func (h *IPTrafficHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

type PortTrafficHeap []PortTraffic

func (h PortTrafficHeap) Len() int { return len(h) }
func (h PortTrafficHeap) Less(i, j int) bool {
	return h[i].Bytes*getUnitFactor(h[i].Unit) < h[j].Bytes*getUnitFactor(h[j].Unit)
}
func (h PortTrafficHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *PortTrafficHeap) Push(x interface{}) {
	*h = append(*h, x.(PortTraffic))
}
func (h *PortTrafficHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

type StatHeap []ProtocolStat

func (h StatHeap) Len() int { return len(h) }
func (h StatHeap) Less(i, j int) bool {
	return h[i].Percent < h[j].Percent
}
func (h StatHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *StatHeap) Push(x interface{}) {
	*h = append(*h, x.(ProtocolStat))
}
func (h *StatHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

func (w *Window) Add(traffic Traffic) {
	key := getKey(traffic.SrcIP, traffic.DstIP, traffic.Protocol, traffic.DstPort)
	w.bucketMu.Lock()
	idx := int(traffic.Timestamp.Unix()/60) % len(w.buckets)
	bucket := w.buckets[idx]
	bucketTs := traffic.Timestamp.Truncate(time.Minute).Unix()
	if bucket.Timestamp != bucketTs {
		bucket.Timestamp = bucketTs
		bucket.Stats = make(map[string]Traffic)
		w.freqStatsMu.Lock()
		for srcIP := range w.srcToDstStats {
			w.srcToDstStats[srcIP] = make(map[string]bool)
		}
		for dstIP := range w.dstToSrcStats {
			w.dstToSrcStats[dstIP] = make(map[string]bool)
		}
		w.freqStatsMu.Unlock()
	}
	s := bucket.Stats[key]
	s.SrcIP = traffic.SrcIP
	s.DstIP = traffic.DstIP
	s.DstPort = traffic.DstPort
	s.Protocol = traffic.Protocol
	s.Bytes += traffic.Bytes
	s.Requests += traffic.Requests
	bucket.Stats[key] = s
	w.bucketMu.Unlock()

	w.freqStatsMu.Lock()
	if _, exists := w.srcToDstStats[traffic.SrcIP]; !exists {
		w.srcToDstStats[traffic.SrcIP] = make(map[string]bool)
	}
	w.srcToDstStats[traffic.SrcIP][traffic.DstIP] = true
	if _, exists := w.dstToSrcStats[traffic.DstIP]; !exists {
		w.dstToSrcStats[traffic.DstIP] = make(map[string]bool)
	}
	w.dstToSrcStats[traffic.DstIP][traffic.SrcIP] = true
	w.freqStatsMu.Unlock()
}

func (w *Window) Summary() []Traffic {
	w.bucketMu.RLock()
	defer w.bucketMu.RUnlock()

	trafficStats := make(map[string]Traffic)
	for _, bucket := range w.buckets {
		if bucket.Timestamp == 0 {
			continue
		}
		for key, stat := range bucket.Stats {
			agg := trafficStats[key]
			agg.SrcIP = stat.SrcIP
			agg.DstIP = stat.DstIP
			agg.DstPort = stat.DstPort
			agg.Protocol = stat.Protocol
			agg.Bytes += stat.Bytes
			agg.Requests += stat.Requests
			trafficStats[key] = agg
		}
	}

	h := &TrafficHeap{}
	heap.Init(h)
	for _, s := range trafficStats {
		if s.Bytes <= 0 {
			continue
		}
		mb := s.Bytes / 1e6
		if mb >= 1000 {
			s.Bytes = mb / 1000
			s.Unit = "GB"
		} else {
			s.Bytes = mb
			s.Unit = "MB"
		}
		if h.Len() < w.Rank {
			heap.Push(h, s)
		} else if s.Bytes*getUnitFactor(s.Unit) > (*h)[0].Bytes*getUnitFactor((*h)[0].Unit) {
			heap.Pop(h)
			heap.Push(h, s)
		}
	}

	result := make([]Traffic, h.Len())
	for i := h.Len() - 1; i >= 0; i-- {
		result[i] = heap.Pop(h).(Traffic)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Bytes*getUnitFactor(result[i].Unit) > result[j].Bytes*getUnitFactor(result[j].Unit)
	})
	return result
}

func (w *Window) IPSummary() []IPTraffic {
	w.bucketMu.RLock()
	defer w.bucketMu.RUnlock()

	ipStatsMap := make(map[string]IPTraffic)
	for _, bucket := range w.buckets {
		if bucket.Timestamp == 0 {
			continue
		}
		for _, stat := range bucket.Stats {
			ipStat := ipStatsMap[stat.SrcIP]
			ipStat.IP = stat.SrcIP
			ipStat.Bytes += stat.Bytes
			ipStat.Requests += stat.Requests
			ipStatsMap[stat.SrcIP] = ipStat
		}
	}

	h := &IPTrafficHeap{}
	heap.Init(h)
	for _, s := range ipStatsMap {
		if s.Bytes <= 0 {
			continue
		}
		mb := s.Bytes / 1e6
		if mb >= 1000 {
			s.Bytes = mb / 1000
			s.Unit = "GB"
		} else {
			s.Bytes = mb
			s.Unit = "MB"
		}
		if h.Len() < w.Rank {
			heap.Push(h, s)
		} else if s.Bytes*getUnitFactor(s.Unit) > (*h)[0].Bytes*getUnitFactor((*h)[0].Unit) {
			heap.Pop(h)
			heap.Push(h, s)
		}
	}

	result := make([]IPTraffic, h.Len())
	for i := h.Len() - 1; i >= 0; i-- {
		result[i] = heap.Pop(h).(IPTraffic)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Bytes*getUnitFactor(result[i].Unit) > result[j].Bytes*getUnitFactor(result[j].Unit)
	})
	return result
}

func (w *Window) PortSummary() []PortTraffic {
	w.bucketMu.RLock()
	defer w.bucketMu.RUnlock()

	portStats := make(map[string]PortTraffic)
	for _, bucket := range w.buckets {
		if bucket.Timestamp == 0 {
			continue
		}
		for _, stat := range bucket.Stats {
			key := stat.DstPort
			portStat := portStats[key]
			portStat.DstPort = stat.DstPort
			portStat.Protocol = stat.Protocol
			portStat.Bytes += stat.Bytes
			portStats[key] = portStat
		}
	}

	h := &PortTrafficHeap{}
	heap.Init(h)
	for _, s := range portStats {
		if s.Bytes <= 0 {
			continue
		}
		mb := s.Bytes / 1e6
		unit := "MB"
		if mb >= 1000 {
			s.Bytes = mb / 1000
			unit = "GB"
		} else {
			s.Bytes = mb
		}
		s.Unit = unit
		if h.Len() < w.Rank {
			heap.Push(h, s)
		} else if s.Bytes*getUnitFactor(s.Unit) > (*h)[0].Bytes*getUnitFactor((*h)[0].Unit) {
			heap.Pop(h)
			heap.Push(h, s)
		}
	}

	result := make([]PortTraffic, h.Len())
	for i := h.Len() - 1; i >= 0; i-- {
		result[i] = heap.Pop(h).(PortTraffic)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Bytes*getUnitFactor(result[i].Unit) > result[j].Bytes*getUnitFactor(result[j].Unit)
	})
	return result
}

func (w *Window) TrendSummary() []TrendItem {
	w.bucketMu.RLock()
	defer w.bucketMu.RUnlock()
	trendMap := make(map[int64]TrendItem)
	for _, bucket := range w.buckets {
		if bucket.Timestamp == 0 {
			continue
		}
		item := TrendItem{
			Timestamp: bucket.Timestamp,
			Bytes:     0,
			Requests:  0,
			Unit:      "MB",
		}
		for _, stat := range bucket.Stats {
			item.Bytes += stat.Bytes
			item.Requests += stat.Requests
		}
		mb := item.Bytes / 1e6
		if mb >= 1000 {
			item.Bytes = mb / 1000
			item.Unit = "GB"
		} else {
			item.Bytes = mb
		}
		trendMap[bucket.Timestamp] = item
	}

	result := make([]TrendItem, 0, len(trendMap))
	for _, item := range trendMap {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp < result[j].Timestamp
	})

	if len(result) < len(w.buckets) {
		now := time.Now().Truncate(time.Minute).Unix()
		for ts := now - int64(w.size.Seconds()) + 60; ts <= now; ts += 60 {
			if _, exists := trendMap[ts]; !exists {
				result = append(result, TrendItem{
					Timestamp: ts,
					Bytes:     0,
					Requests:  0,
					Unit:      "MB",
				})
			}
		}
		sort.Slice(result, func(i, j int) bool {
			return result[i].Timestamp < result[j].Timestamp
		})
	}

	return result
}

func (w *Window) ProtocolSummary() ProtocolCache {
	w.bucketMu.RLock()
	defer w.bucketMu.RUnlock()

	protoBytes := make(map[string]float64)
	portBytes := make(map[string]float64)
	totalBytes := 0.0
	statCount := 0
	for _, bucket := range w.buckets {
		if bucket.Timestamp == 0 {
			continue
		}
		statCount += len(bucket.Stats)
		for _, stat := range bucket.Stats {
			proto := extractProtocol(stat.DstPort)
			if len(proto) == 0 {
				continue
			}
			protoBytes[proto] += stat.Bytes
			portBytes[stat.DstPort] += stat.Bytes
			totalBytes += stat.Bytes
		}
	}

	protoHeap := &StatHeap{}
	heap.Init(protoHeap)
	for proto, bytes := range protoBytes {
		if bytes <= 0 {
			continue
		}
		percent := 0.0
		if totalBytes > 0 {
			percent = (bytes / totalBytes) * 100
			percent = math.Round(percent*100) / 100
		}
		stat := ProtocolStat{Protocol: proto, Percent: percent}
		if protoHeap.Len() < 5 {
			heap.Push(protoHeap, stat)
		} else if percent > (*protoHeap)[0].Percent {
			heap.Pop(protoHeap)
			heap.Push(protoHeap, stat)
		}
	}

	protocolStats := make([]ProtocolStat, 0, protoHeap.Len())
	for protoHeap.Len() > 0 {
		stat := heap.Pop(protoHeap).(ProtocolStat)
		protocolStats = append(protocolStats, stat)
	}
	sort.Slice(protocolStats, func(i, j int) bool {
		return protocolStats[i].Percent > protocolStats[j].Percent
	})

	portHeap := &PortTrafficHeap{}
	heap.Init(portHeap)
	for port, bytes := range portBytes {
		if bytes <= 0 {
			continue
		}
		mb := bytes / 1e6
		unit := "MB"
		if mb >= 1000 {
			mb = mb / 1000
			unit = "GB"
		}
		pt := PortTraffic{DstPort: port, Bytes: mb, Unit: unit}
		if portHeap.Len() < 5 {
			heap.Push(portHeap, pt)
		} else if bytes > (*portHeap)[0].Bytes*getUnitFactor((*portHeap)[0].Unit) {
			heap.Pop(portHeap)
			heap.Push(portHeap, pt)
		}
	}

	portStats := make([]ProtocolStat, 0, 5)
	for portHeap.Len() > 0 {
		pt := heap.Pop(portHeap).(PortTraffic)
		bytes := pt.Bytes * getUnitFactor(pt.Unit)
		percent := 0.0
		if totalBytes > 0 {
			percent = (bytes / totalBytes) * 100
			percent = math.Round(percent*100) / 100
		}
		portStats = append(portStats, ProtocolStat{
			Protocol: extractPort(pt.DstPort),
			Percent:  percent,
		})
	}
	return ProtocolCache{
		ProtocolStats: protocolStats,
		PortStats:     portStats,
	}
}

func (w *Window) StartCacheUpdate(wg *sync.WaitGroup) {
	defer wg.Done()
	ticker := time.NewTicker(2 * time.Second)
	for {
		select {
		case <-ticker.C:
			result := w.Summary()
			ipResult := w.IPSummary()
			portResult := w.PortSummary()
			trendResult := w.TrendSummary()
			protoResult := w.ProtocolSummary()
			w.cacheMu.Lock()
			w.cache = result
			w.ipCache = ipResult
			w.portCache = portResult
			w.trendCache = trendResult
			w.protoCache = protoResult
			w.lastCalc = time.Now()
			w.cacheMu.Unlock()
		case <-utils.Ctx.Done():
			ticker.Stop()
			return
		}
	}
}

func Capture(device string, pool *async.WorkerPool, w *Window) {
	handle, err := pcap.OpenLive(device, 2048, true, pcap.BlockForever)
	if err != nil {
		notSpecified := strings.Contains(err.Error(), "not been specified")
		errEth := strings.Contains(err.Error(), "Error opening adapter")
		notFound := strings.Contains(err.Error(), "No such device exists")
		if notFound || errEth || notSpecified {
			fmt.Printf("网卡 %s 不存在或无法打开，请检查配置文件中的网卡名称是否正确。\n", device)
			fmt.Printf("可用网卡列表：\n")
			utils.ListAvailableDevices()
			os.Exit(1)
		}
		panic(err)
	}
	defer handle.Close()

	if err = handle.SetBPFFilter("tcp or udp"); err != nil {
		panic(err)
	}
	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())
	ticker := time.NewTicker(time.Minute)
	defer func() {
		ticker.Stop()
		pool.Wg.Done()
	}()
	for {
		select {
		case packet := <-packetSource.Packets():
			pool.Add(func(args ...interface{}) error {
				pt := args[0].(gopacket.Packet)
				var ipStr, dstIP, protocol string
				var dstPort string
				if ipLayer := pt.Layer(layers.LayerTypeIPv4); ipLayer != nil {
					ip, _ := ipLayer.(*layers.IPv4)
					ipStr = ip.SrcIP.String()
					dstIP = ip.DstIP.String()
				} else {
					return nil
				}
				if tcpLayer := pt.Layer(layers.LayerTypeTCP); tcpLayer != nil {
					tcp, _ := tcpLayer.(*layers.TCP)
					protocol = "TCP"
					dstPort = tcp.DstPort.String()
				} else if udpLayer := pt.Layer(layers.LayerTypeUDP); udpLayer != nil {
					udp, _ := udpLayer.(*layers.UDP)
					protocol = "UDP"
					dstPort = udp.DstPort.String()
				} else {
					return nil
				}
				if !utils.IsValidIP(ipStr) || !utils.IsValidIP(dstIP) {
					return nil
				}
				packetLen := int64(pt.Metadata().Length)
				stats := syncPool.Get().(*Traffic)
				*stats = Traffic{
					Timestamp: pt.Metadata().Timestamp,
					SrcIP:     ipStr,
					DstIP:     dstIP,
					DstPort:   dstPort,
					Protocol:  protocol,
					Bytes:     float64(packetLen),
					Requests:  1,
				}
				copied := *stats
				syncPool.Put(stats)
				if conf.CoreConf.Kafka.Enable {
					msg, _ := json.Marshal(copied)
					kafka.Push(msg)
				}
				w.Add(copied)
				return nil
			}, packet)
		case <-ticker.C:
			now := time.Now().Format(utils.TimeLayout)
			var (
				bws []notify.Bandwidth
				fqs []notify.Frequency
			)
			thresholdBytes := getThresholdBytes()
			w.cacheMu.RLock()
			for _, s := range w.ipCache {
				bytes := s.Bytes * getUnitFactor(s.Unit)
				if bytes > thresholdBytes && !notify.IsWhiteIp(s.IP) {
					bws = append(bws, notify.Bandwidth{
						IP:        s.IP,
						Bandwidth: fmt.Sprintf("%.2f%s", s.Bytes, s.Unit),
					})
				}
			}
			if len(bws) != 0 {
				notify.Push(notify.DdosAlert{
					BandwidthS: bws,
					Timestamp:  now,
					Title:      highBandwidth,
					Location:   conf.CoreConf.Notify.Location,
					TimeRange:  utils.GetTimeRangeString(conf.CoreConf.Server.Size),
				})
			}
			w.cacheMu.RUnlock()

			frequencyThreshold := conf.CoreConf.Notify.FrequencyThreshold
			w.freqStatsMu.RLock()
			for srcIP, dstIPs := range w.srcToDstStats {
				if len(dstIPs) > frequencyThreshold && !notify.IsWhiteIp(srcIP) {
					fqs = append(fqs, notify.Frequency{
						IP:    srcIP,
						Count: len(dstIPs),
						Desc:  fmt.Sprintf("检测到源IP %s 在最近1分钟内尝试连接 %d 个不同目标 IP，疑似异常行为", srcIP, len(dstIPs)),
					})
				}
			}
			for dstIP, srcIPs := range w.dstToSrcStats {
				if len(srcIPs) > frequencyThreshold && !notify.IsWhiteIp(dstIP) {
					fqs = append(fqs, notify.Frequency{
						IP:    dstIP,
						Count: len(srcIPs),
						Desc:  fmt.Sprintf("检测到目标IP %s 在最近1分钟内接收来自 %d 个不同源IP访问请求，疑似异常行为", dstIP, len(srcIPs)),
					})
				}
			}
			if len(fqs) != 0 {
				notify.Push(notify.DdosAlert{
					FrequencyS: fqs,
					Timestamp:  now,
					Title:      highFrequency,
					Location:   conf.CoreConf.Notify.Location,
					TimeRange:  utils.GetTimeRangeString(1),
				})
			}
			w.freqStatsMu.RUnlock()
		case <-utils.Ctx.Done():
			return
		}
	}
}

func (w *Window) ServeHTTP(wr http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/top" {
		var (
			page = 1
			size = 10
		)
		w.cacheMu.RLock()
		top := w.cache
		w.cacheMu.RUnlock()
		pageStr := r.URL.Query().Get("page")
		sizeStr := r.URL.Query().Get("size")
		keyword := r.URL.Query().Get("keyword")
		if len(pageStr) != 0 {
			page = cast.ToInt(pageStr)
		}
		if len(sizeStr) != 0 {
			size = cast.ToInt(sizeStr)
		}
		resp := make([]TopItem, 0, len(top))
		for _, s := range top {
			matches := true
			if len(keyword) != 0 {
				if !strings.Contains(s.SrcIP, keyword) && !strings.Contains(s.DstIP, keyword) &&
					!strings.Contains(s.DstPort, keyword) {
					matches = false
				}
			}
			if matches {
				resp = append(resp, TopItem{
					SrcIP:     s.SrcIP,
					DstIP:     s.DstIP,
					DstPort:   s.DstPort,
					Protocol:  s.Protocol,
					Bandwidth: s.Bytes,
					Unit:      s.Unit,
					Requests:  s.Requests,
				})
			}
		}
		start, end := paginate(page, size, len(resp))
		response := TopResponse{
			Base: Base{
				Page:  page,
				Size:  size,
				Total: len(resp),
				Pages: int(math.Ceil(float64(len(resp)) / float64(size))),
			},
			Data:       resp[start:end],
			Timestamp:  w.lastCalc.Unix(),
			WindowSize: int(w.size.Seconds()),
			Rank:       w.Rank,
		}
		wr.Header().Set("Content-Type", "application/json")
		if err := ji.NewEncoder(wr).Encode(response); err != nil {
			http.Error(wr, "Failed to encode JSON", http.StatusInternalServerError)
		}
		return
	}
	if r.URL.Path == "/ip_top" {
		var (
			page = 1
			size = 10
		)
		w.cacheMu.RLock()
		top := w.ipCache
		w.cacheMu.RUnlock()
		pageStr := r.URL.Query().Get("page")
		sizeStr := r.URL.Query().Get("size")
		keyword := r.URL.Query().Get("keyword")
		if len(pageStr) != 0 {
			page = cast.ToInt(pageStr)
		}
		if len(sizeStr) != 0 {
			size = cast.ToInt(sizeStr)
		}
		resp := make([]IPTraffic, 0, len(top))
		for _, s := range top {
			matches := true
			if len(keyword) != 0 {
				if !strings.Contains(s.IP, keyword) {
					matches = false
				}
			}
			if matches {
				resp = append(resp, IPTraffic{
					IP:       s.IP,
					Unit:     s.Unit,
					Requests: s.Requests,
					Bytes:    s.Bytes,
				})
			}
		}
		start, end := paginate(page, size, len(resp))
		response := IPTrafficResponse{
			Base: Base{
				Page:  page,
				Size:  size,
				Total: len(resp),
				Pages: int(math.Ceil(float64(len(resp)) / float64(size))),
			},
			Data: resp[start:end],
		}
		wr.Header().Set("Content-Type", "application/json")
		if err := ji.NewEncoder(wr).Encode(response); err != nil {
			http.Error(wr, "Failed to encode JSON", http.StatusInternalServerError)
		}
		return
	}
	if r.URL.Path == "/stats/ports" {
		var (
			page = 1
			size = 10
		)
		w.cacheMu.RLock()
		top := w.portCache
		w.cacheMu.RUnlock()
		pageStr := r.URL.Query().Get("page")
		sizeStr := r.URL.Query().Get("size")
		keyword := r.URL.Query().Get("keyword")
		if len(pageStr) != 0 {
			page = cast.ToInt(pageStr)
		}
		if len(sizeStr) != 0 {
			size = cast.ToInt(sizeStr)
		}
		resp := make([]PortItem, 0, len(top))
		for _, s := range top {
			matches := true
			if len(keyword) != 0 {
				if !strings.Contains(s.DstPort, keyword) {
					matches = false
				}
			}
			if matches {
				resp = append(resp, PortItem{
					DstPort:  s.DstPort,
					Bytes:    s.Bytes,
					Protocol: s.Protocol,
					Unit:     s.Unit,
				})
			}
		}
		start, end := paginate(page, size, len(resp))
		response := PortItemResponse{
			Base: Base{
				Page:  page,
				Size:  size,
				Total: len(resp),
				Pages: int(math.Ceil(float64(len(resp)) / float64(size))),
			},
			Data: resp[start:end],
		}
		wr.Header().Set("Content-Type", "application/json")
		if err := ji.NewEncoder(wr).Encode(response); err != nil {
			http.Error(wr, "Failed to encode JSON", http.StatusInternalServerError)
		}
		return
	}
	if r.URL.Path == "/trend" {
		w.cacheMu.RLock()
		trend := w.trendCache
		w.cacheMu.RUnlock()
		response := TrendResponse{
			Data:       trend,
			WindowSize: int(w.size.Seconds()),
		}
		wr.Header().Set("Content-Type", "application/json")
		if err := ji.NewEncoder(wr).Encode(response); err != nil {
			http.Error(wr, "Failed to encode JSON", http.StatusInternalServerError)
		}
		return
	}
	if r.URL.Path == "/sys" {
		response := systemMonitorManager.Get(localIp)
		wr.Header().Set("Content-Type", "application/json")
		if err := ji.NewEncoder(wr).Encode(response); err != nil {
			http.Error(wr, "Failed to encode JSON", http.StatusInternalServerError)
		}
		return
	}
	if r.URL.Path == "/stats/protocols" {
		w.cacheMu.RLock()
		protoCache := w.protoCache
		w.cacheMu.RUnlock()
		response := ProtocolResponse{
			ProtocolStats: protoCache.ProtocolStats,
			PortStats:     protoCache.PortStats,
			Timestamp:     w.lastCalc.Unix(),
			WindowSize:    int(w.size.Seconds()),
		}
		wr.Header().Set("Content-Type", "application/json")
		if err := ji.NewEncoder(wr).Encode(response); err != nil {
			http.Error(wr, "Failed to encode JSON", http.StatusInternalServerError)
		}
		return
	}
	if r.URL.Path == "/" {
		file, err := content.ReadFile("index.html")
		if err != nil {
			http.Error(wr, "Failed to read index.html", http.StatusInternalServerError)
			return
		}
		wr.Header().Set("Content-Type", "text/html")
		_, _ = wr.Write(file)
		return
	}
	http.FileServer(http.FS(content)).ServeHTTP(wr, r)
}

func getKey(ip, dstIP, protocol string, dstPort string) string {
	return fmt.Sprintf("%s|%s|%s|%s", ip, dstIP, protocol, dstPort)
}

func getUnitFactor(unit string) float64 {
	switch unit {
	case "GB":
		return 1e9
	case "MB":
		return 1e6
	default:
		return 1
	}
}

func getThresholdBytes() float64 {
	var (
		unit  = conf.CoreConf.Notify.ThresholdUnit
		value = conf.CoreConf.Notify.ThresholdValue
	)
	switch unit {
	case "GB":
		return value * 1e9
	case "MB":
		return value * 1e6
	default:
		return value
	}
}

func extractPort(dstPort string) string {
	if strings.Contains(dstPort, "(") {
		return dstPort[:strings.Index(dstPort, "(")]
	}
	return dstPort
}

func extractProtocol(dstPort string) string {
	if strings.Contains(dstPort, "(") && strings.HasSuffix(dstPort, ")") {
		start := strings.Index(dstPort, "(") + 1
		end := len(dstPort) - 1
		return strings.ToLower(dstPort[start:end])
	}
	return ""
}

//go:embed index.html
//go:embed static/*
var content embed.FS

type TopItem struct {
	SrcIP     string  `json:"src_ip"`
	DstIP     string  `json:"dest_ip"`
	DstPort   string  `json:"dest_port"`
	Protocol  string  `json:"protocol"`
	Bandwidth float64 `json:"bandwidth"`
	Unit      string  `json:"unit"`
	Requests  int64   `json:"requests"`
}

type TopResponse struct {
	Base
	Data       []TopItem `json:"data"`
	Timestamp  int64     `json:"timestamp"`
	WindowSize int       `json:"window_size"`
	Rank       int       `json:"rank"`
}

type PortItem struct {
	DstPort  string  `json:"dest_port"`
	Bytes    float64 `json:"bytes"`
	Protocol string  `json:"protocol"`
	Unit     string  `json:"unit"`
}

type PortItemResponse struct {
	Data []PortItem `json:"data"`
	Base
}

type TrendResponse struct {
	Data       []TrendItem `json:"data"`
	WindowSize int         `json:"window_size"`
}

type ProtocolStat struct {
	Protocol string  `json:"protocol"`
	Percent  float64 `json:"percent"`
}

type ProtocolResponse struct {
	ProtocolStats []ProtocolStat `json:"protocol_stats"`
	PortStats     []ProtocolStat `json:"port_stats"`
	Timestamp     int64          `json:"timestamp"`
	WindowSize    int            `json:"window_size"`
}

type ProtocolCache struct {
	ProtocolStats []ProtocolStat
	PortStats     []ProtocolStat
}

var (
	systemMonitorManager *SystemMonitorManager
	localIp              = utils.GetLocalIp()
)

type SystemMonitor struct {
	NetworkInterface string `json:"network_interface"`
	LocalIp          string `json:"local_ip"`
	Cpu              string `json:"cpu"`
	Mem              string `json:"mem"`
	Workers          int    `json:"workers"`
}

type SystemMonitorManager struct {
	Mu   sync.RWMutex
	Data map[string]SystemMonitor
}

func (s *SystemMonitorManager) Get(key string) SystemMonitor {
	s.Mu.RLock()
	defer s.Mu.RUnlock()
	return s.Data[key]
}

func (s *SystemMonitorManager) Set(key string, value SystemMonitor) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	s.Data[key] = value
}

func init() {
	systemMonitorManager = &SystemMonitorManager{
		Data: make(map[string]SystemMonitor),
	}
	go systemMonitorManager.run()
}

func (s *SystemMonitorManager) run() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.Set(localIp, SystemMonitor{
				NetworkInterface: conf.CoreConf.Server.Eth,
				LocalIp:          localIp,
				Cpu:              utils.GetCpuUsage(),
				Mem:              utils.GetMemUsage(),
				Workers:          runtime.NumGoroutine(),
			})
		case <-utils.Ctx.Done():
			return
		}
	}
}
