package main

import (
	"container/heap"
	"context"
	"fmt"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"github.com/xxddpac/async"
	"sort"
	"sync"
	"time"
)

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

type Traffic struct {
	Timestamp time.Time `json:"-"`
	SrcIP     string    `json:"src_ip"`
	DstIP     string    `json:"dest_ip"`
	DstPort   uint16    `json:"dest_port"`
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

type PortTraffic struct {
	Protocol string  `json:"protocol"`
	DstPort  uint16  `json:"dest_port"`
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
	buckets    []*Bucket
	size       time.Duration
	bucketMu   sync.RWMutex
	ipStats    map[string]Traffic
	statsMu    sync.RWMutex
	cache      []Traffic
	ipCache    []IPTraffic
	trendCache []TrendItem
	cacheMu    sync.RWMutex
	lastCalc   time.Time
	Rank       int
	portStats  map[uint16]PortTraffic
	portCache  []PortTraffic
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
		buckets:   buckets,
		size:      size,
		portStats: make(map[uint16]PortTraffic),
		ipStats:   make(map[string]Traffic),
		Rank:      rank,
	}
}

func (w *Window) Add(traffic Traffic) {
	key := getKey(traffic.SrcIP, traffic.DstIP, traffic.Protocol, traffic.DstPort)
	w.bucketMu.Lock()
	idx := int(traffic.Timestamp.Unix()/60) % len(w.buckets)
	bucket := w.buckets[idx]
	bucketTs := traffic.Timestamp.Truncate(time.Minute).Unix()
	if bucket.Timestamp != bucketTs {
		bucket.Timestamp = bucketTs
		w.statsMu.Lock()
		for k := range bucket.Stats {
			if stat, ok := w.ipStats[k]; ok {
				stat.Bytes -= bucket.Stats[k].Bytes
				stat.Requests -= bucket.Stats[k].Requests
				if stat.Bytes <= 1e-6 {
					delete(w.ipStats, k)
				} else {
					w.ipStats[k] = stat
				}
			}
		}
		bucket.Stats = make(map[string]Traffic)
		w.statsMu.Unlock()
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

	w.statsMu.Lock()
	agg := w.ipStats[key]
	agg.SrcIP = traffic.SrcIP
	agg.DstIP = traffic.DstIP
	agg.DstPort = traffic.DstPort
	agg.Protocol = traffic.Protocol
	agg.Bytes += traffic.Bytes
	agg.Requests += traffic.Requests
	w.ipStats[key] = agg

	portStat := w.portStats[traffic.DstPort]
	portStat.DstPort = traffic.DstPort
	portStat.Bytes += traffic.Bytes
	portStat.Protocol = traffic.Protocol
	w.portStats[traffic.DstPort] = portStat
	w.statsMu.Unlock()
}

func (w *Window) PortSummary() []PortTraffic {
	w.statsMu.RLock()
	portStats := make(map[uint16]PortTraffic, len(w.portStats))
	totalBytes := 0.0
	for port, s := range w.portStats {
		if s.Bytes > 0 {
			portStats[port] = s
			totalBytes += s.Bytes
		}
	}
	w.statsMu.RUnlock()

	h := &PortTrafficHeap{}
	heap.Init(h)
	for _, s := range portStats {
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

func (w *Window) Summary() []Traffic {
	w.statsMu.RLock()
	ipStats := make(map[string]Traffic, len(w.ipStats))
	for k, s := range w.ipStats {
		if s.Bytes > 0 {
			ipStats[k] = s
		}
	}
	w.statsMu.RUnlock()
	h := &TrafficHeap{}
	heap.Init(h)
	for _, s := range ipStats {
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
	w.statsMu.RLock()
	ipStatsMap := make(map[string]IPTraffic)
	for _, s := range w.ipStats {
		if s.Bytes > 0 {
			ipStat := ipStatsMap[s.SrcIP]
			ipStat.IP = s.SrcIP
			ipStat.Bytes += s.Bytes
			ipStat.Requests += s.Requests
			ipStatsMap[s.SrcIP] = ipStat
		}
	}
	w.statsMu.RUnlock()

	h := &IPTrafficHeap{}
	heap.Init(h)
	for _, s := range ipStatsMap {
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

func (w *Window) StartCacheUpdate(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	ticker := time.NewTicker(2 * time.Second)
	for {
		select {
		case <-ticker.C:
			result := w.Summary()
			ipResult := w.IPSummary()
			portResult := w.PortSummary()
			trendResult := w.TrendSummary()
			w.cacheMu.Lock()
			w.cache = result
			w.ipCache = ipResult
			w.portCache = portResult
			w.trendCache = trendResult
			w.lastCalc = time.Now()
			w.cacheMu.Unlock()
		case <-ctx.Done():
			ticker.Stop()
			return
		}
	}
}

func capture(ctx context.Context, device string, pool *async.WorkerPool, w *Window) {
	handle, err := pcap.OpenLive(device, 2048, true, pcap.BlockForever)
	if err != nil {
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
				var dstPort uint16
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
					dstPort = uint16(tcp.DstPort)
				} else if udpLayer := pt.Layer(layers.LayerTypeUDP); udpLayer != nil {
					udp, _ := udpLayer.(*layers.UDP)
					protocol = "UDP"
					dstPort = uint16(udp.DstPort)
				} else {
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
				w.Add(*stats)
				syncPool.Put(stats)
				return nil
			}, packet)
		case <-ticker.C:
			//todo 【alert】 high bandwidth for single ip
			//for _, item := range w.ipCache {
			//	if item.Bytes > 1000 {
			//		pool.Logger.Printf("+++++ high bandwidth ip: %s +++++", item.IP)
			//	}
			//}
		case <-ctx.Done():
			return
		}
	}
}

func getKey(ip, dstIP, protocol string, dstPort uint16) string {
	return fmt.Sprintf("%s|%s|%s|%d", ip, dstIP, protocol, dstPort)
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
