package flow

import (
	"container/heap"
	"embed"
	"encoding/json"
	"fmt"
	"github.com/google/gopacket"
	"github.com/google/gopacket/afpacket"
	"github.com/google/gopacket/layers"
	ji "github.com/json-iterator/go"
	"github.com/panjf2000/ants/v2"
	"github.com/spf13/cast"
	"go-flow/conf"
	"go-flow/kafka"
	"go-flow/notify"
	"go-flow/utils"
	"go-flow/zlog"
	"log"
	"math"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	syncPool       = sync.Pool{New: func() interface{} { return &Traffic{} }}
	counter  int64 = 0
)

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
	Desc     string  `json:"desc"`
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
	startTime     time.Time
	endTime       time.Time
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

		if w.startTime.IsZero() || bucketTs < w.startTime.Unix() {
			w.startTime = time.Unix(bucketTs, 0)
		}
		if time.Unix(bucketTs, 0).After(w.endTime) {
			w.endTime = time.Unix(bucketTs, 0)
		}

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

func (w *Window) StartCacheUpdate(wg *sync.WaitGroup) {
	defer wg.Done()
	ticker := time.NewTicker(time.Second * time.Duration(conf.CoreConf.Server.Interval))
	for {
		select {
		case <-ticker.C:
			// deep copy
			w.bucketMu.RLock()
			bucketsCopy := make([]*Bucket, len(w.buckets))
			for i, b := range w.buckets {
				if b == nil {
					continue
				}
				statsCopy := make(map[string]Traffic, len(b.Stats))
				for k, v := range b.Stats {
					statsCopy[k] = v
				}
				bucketsCopy[i] = &Bucket{
					Timestamp: b.Timestamp,
					Stats:     statsCopy,
				}
			}
			w.bucketMu.RUnlock()

			traffic := topTrafficHeap(aggregateTrafficStats(bucketsCopy), w.Rank)
			ip := topIPHeap(aggregateIPStats(bucketsCopy), w.Rank)
			port := topPortHeap(aggregatePortStats(bucketsCopy), w.Rank)
			trend := completeTrend(aggregateTrendMap(bucketsCopy), len(w.buckets))
			proto := topProtoStatsFromSnapshot(bucketsCopy)

			w.cacheMu.Lock()
			w.cache = traffic
			w.ipCache = ip
			w.portCache = port
			w.trendCache = trend
			w.protoCache = proto
			w.lastCalc = time.Now()
			w.cacheMu.Unlock()
		case <-utils.Ctx.Done():
			ticker.Stop()
			return
		}
	}
}

func aggregateTrafficStats(buckets []*Bucket) map[string]Traffic {
	trafficStats := make(map[string]Traffic)
	for _, bucket := range buckets {
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
	return trafficStats
}

func aggregateIPStats(buckets []*Bucket) map[string]IPTraffic {
	ipStatsMap := make(map[string]IPTraffic)
	for _, bucket := range buckets {
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
	return ipStatsMap
}

func aggregatePortStats(buckets []*Bucket) map[string]PortTraffic {
	portStats := make(map[string]PortTraffic)
	for _, bucket := range buckets {
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
	return portStats
}

func aggregateTrendMap(buckets []*Bucket) map[int64]TrendItem {
	trendMap := make(map[int64]TrendItem)
	for _, bucket := range buckets {
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
		item.Bytes = item.Bytes / 1e6
		trendMap[bucket.Timestamp] = item
	}
	return trendMap
}

func topTrafficHeap(m map[string]Traffic, rank int) []Traffic {
	h := &TrafficHeap{}
	heap.Init(h)
	for _, s := range m {
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
		if h.Len() < rank {
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
	return result
}

func topIPHeap(m map[string]IPTraffic, rank int) []IPTraffic {
	h := &IPTrafficHeap{}
	heap.Init(h)
	for _, s := range m {
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
		if h.Len() < rank {
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
	return result
}

func topPortHeap(m map[string]PortTraffic, rank int) []PortTraffic {
	h := &PortTrafficHeap{}
	heap.Init(h)
	for _, s := range m {
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
		if h.Len() < rank {
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
	return result
}

func completeTrend(trendMap map[int64]TrendItem, total int) []TrendItem {
	result := make([]TrendItem, 0, len(trendMap))
	for _, item := range trendMap {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp < result[j].Timestamp
	})
	if len(result) < total {
		now := time.Now().Truncate(time.Minute).Unix()
		for ts := now - int64(total)*60 + 60; ts <= now; ts += 60 {
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

func topProtoStatsFromSnapshot(buckets []*Bucket) ProtocolCache {
	protoBytes := make(map[string]float64)
	portBytes := make(map[string]float64)
	totalBytes := 0.0
	for _, bucket := range buckets {
		if bucket.Timestamp == 0 {
			continue
		}
		for _, stat := range bucket.Stats {
			proto := extractProtocol(stat.DstPort)
			if proto == "" {
				continue
			}
			protoBytes[proto] += float64(stat.Bytes)
			portBytes[stat.DstPort] += float64(stat.Bytes)
			totalBytes += float64(stat.Bytes)
		}
	}
	return topProtoStats(protoBytes, portBytes, totalBytes)
}

func topProtoStats(protoBytes, portBytes map[string]float64, totalBytes float64) ProtocolCache {
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
		protocolStats = append(protocolStats, heap.Pop(protoHeap).(ProtocolStat))
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

type PacketData struct {
	Data      []byte
	Timestamp time.Time
}

func Capture(device string, w *Window, wg *sync.WaitGroup) {
	var (
		packetChan   = make(chan PacketData, conf.CoreConf.Server.PacketChan)
		err          error
		batchSize    = 1000
		numConsumers = runtime.NumCPU()
		producerWg   sync.WaitGroup
		consumerWg   sync.WaitGroup
	)

	defer wg.Done()
	// init goroutine pool
	pool, err := ants.NewPoolWithFunc(conf.CoreConf.Server.Workers, func(payload interface{}) {
		batch := payload.([]PacketData)
		for _, pkt := range batch {
			packet := gopacket.NewPacket(pkt.Data, layers.LayerTypeEthernet, gopacket.DecodeOptions{
				Lazy:   false,
				NoCopy: false,
			})
			if packet == nil {
				return
			}
			packet.Metadata().CaptureLength = len(pkt.Data)
			packet.Metadata().Length = len(pkt.Data)

			var ipStr, dstIP, protocol, dstPort string
			if ipLayer := packet.Layer(layers.LayerTypeIPv4); ipLayer != nil {
				ip := ipLayer.(*layers.IPv4)
				ipStr = ip.SrcIP.String()
				dstIP = ip.DstIP.String()
			} else {
				return
			}
			if tcpLayer := packet.Layer(layers.LayerTypeTCP); tcpLayer != nil {
				tcp := tcpLayer.(*layers.TCP)
				protocol = "TCP"
				dstPort = tcp.DstPort.String()
			} else if udpLayer := packet.Layer(layers.LayerTypeUDP); udpLayer != nil {
				udp := udpLayer.(*layers.UDP)
				protocol = "UDP"
				dstPort = udp.DstPort.String()
			} else {
				return
			}
			if !utils.IsValidIP(ipStr) || !utils.IsValidIP(dstIP) {
				return
			}
			packetLen := int64(packet.Metadata().Length)
			stats := syncPool.Get().(*Traffic)
			*stats = Traffic{
				Timestamp: pkt.Timestamp,
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
		}
	}, ants.WithMaxBlockingTasks(conf.CoreConf.Server.Workers*20))
	if err != nil {
		log.Fatalf("Failed to create goroutine pool: %v", err)
	}
	defer pool.Release()

	tPacket, err := afpacket.NewTPacket(
		afpacket.OptInterface(device),
		afpacket.OptFrameSize(65536),
		afpacket.OptBlockSize(1<<22),
		afpacket.OptNumBlocks(64),
		afpacket.OptPollTimeout(100*time.Millisecond),
		afpacket.OptTPacketVersion(afpacket.TPacketVersion3),
	)
	if err != nil {
		log.Fatalf("Failed to create TPACKET_V3: %v", err)
	}

	// monitor drop packets and send big bandwidth and high frequency alerts
	go func() {
		var monitorTicker = time.NewTicker(time.Minute)
		defer monitorTicker.Stop()
		for {
			select {
			case <-utils.Ctx.Done():
				return
			case <-monitorTicker.C:
				// monitor dropped packets
				count := atomic.SwapInt64(&counter, 0)
				if count != 0 {
					zlog.Infof("COUNT", "Dropped packets: %d", count)
				}
				if !conf.CoreConf.Notify.Enable {
					continue
				}
				// alert notification for Big Bandwidth and High Frequency
				now := time.Now().Format(utils.TimeLayout)
				var (
					bws []notify.Bandwidth
					fqs []notify.Frequency
				)
				thresholdBytes := getThresholdBytes()
				var snapshot []IPTraffic

				w.cacheMu.RLock()
				snapshot = append([]IPTraffic(nil), w.ipCache...)
				w.cacheMu.RUnlock()

				for _, s := range snapshot {
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

				frequencyThreshold := conf.CoreConf.Notify.FrequencyThreshold

				w.freqStatsMu.RLock()
				srcCopy := make(map[string]map[string]bool, len(w.srcToDstStats))
				for src, dstMap := range w.srcToDstStats {
					dstCopy := make(map[string]bool, len(dstMap))
					for dst := range dstMap {
						dstCopy[dst] = true
					}
					srcCopy[src] = dstCopy
				}
				dstCopy := make(map[string]map[string]bool, len(w.dstToSrcStats))
				for dst, srcMap := range w.dstToSrcStats {
					srcS := make(map[string]bool, len(srcMap))
					for src := range srcMap {
						srcS[src] = true
					}
					dstCopy[dst] = srcS
				}
				w.freqStatsMu.RUnlock()

				for srcIP, dstIPs := range srcCopy {
					if len(dstIPs) > frequencyThreshold && !notify.IsWhiteIp(srcIP) {
						fqs = append(fqs, notify.Frequency{
							IP:    srcIP,
							Count: len(dstIPs),
							Desc:  fmt.Sprintf("检测到源IP %s 在最近1分钟内尝试连接 %d 个不同目标 IP，疑似异常行为", srcIP, len(dstIPs)),
						})
					}
				}
				for dstIP, srcIPs := range dstCopy {
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
			}
		}
	}()

	// producer
	producerWg.Add(1)
	go func() {
		defer producerWg.Done()
		defer close(packetChan)
		packetSource := gopacket.NewPacketSource(tPacket, layers.LayerTypeEthernet)
		for {
			select {
			case <-utils.Ctx.Done():
				return
			default:
				packet, err := packetSource.NextPacket()
				if err != nil {
					continue
				}
				data := packet.Data()
				buf := make([]byte, len(data))
				copy(buf, data)

				select {
				case packetChan <- PacketData{
					Data:      buf,
					Timestamp: packet.Metadata().Timestamp,
				}:
				default:
					atomic.AddInt64(&counter, 1)
				}
			}
		}
	}()

	// consumer
	// use goroutine pool to process packets in batches
	for i := 0; i < numConsumers; i++ {
		consumerWg.Add(1)
		go func() {
			defer consumerWg.Done()
			batch := make([]PacketData, 0, batchSize)
			ticker := time.NewTicker(2 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case pkt, ok := <-packetChan:
					if !ok {
						if len(batch) > 0 {
							copyBatch := append([]PacketData{}, batch...)
							_ = pool.Invoke(copyBatch)
						}
						return
					}
					batch = append(batch, pkt)
					if len(batch) >= batchSize {
						copyBatch := append([]PacketData{}, batch...)
						_ = pool.Invoke(copyBatch)
						batch = batch[:0]
					}
				case <-ticker.C:
					if len(batch) > 0 {
						copyBatch := append([]PacketData{}, batch...)
						_ = pool.Invoke(copyBatch)
						batch = batch[:0]
					}
				case <-utils.Ctx.Done():
					return
				}
			}
		}()
	}

	producerWg.Wait()
	consumerWg.Wait()

	tPacket.Close()
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
					Desc:      fmt.Sprintf("%v%v", fmt.Sprintf("%0.2f", s.Bytes)+s.Unit, w.Mbps(s.Bytes, s.Unit)),
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
					Desc:     fmt.Sprintf("%v%v", fmt.Sprintf("%0.2f", s.Bytes)+s.Unit, w.Mbps(s.Bytes, s.Unit)),
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
		response := systemMonitorManager.Get(utils.LocalIpAddr)
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
	Desc      string  `json:"desc"`
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
			s.Set(utils.LocalIpAddr, SystemMonitor{
				NetworkInterface: conf.CoreConf.Server.Eth,
				LocalIp:          utils.LocalIpAddr,
				Cpu:              utils.GetCpuUsage(),
				Mem:              utils.GetMemUsage(),
				Workers:          runtime.NumGoroutine(),
			})
		case <-utils.Ctx.Done():
			return
		}
	}
}

func (w *Window) Mbps(bytes float64, unit string) string {
	duration := w.endTime.Sub(w.startTime)
	if duration <= 0 {
		return ""
	}
	if duration > w.size {
		duration = w.size
	}
	var bits float64
	switch unit {
	case "MB":
		bits = bytes * 8 * 1_000_000
	case "GB":
		bits = bytes * 8 * 1_000_000_000
	default:
		return ""
	}
	mbps := bits / duration.Seconds() / 1_000_000
	return fmt.Sprintf("  ( %.2fMbps )", mbps)
}
