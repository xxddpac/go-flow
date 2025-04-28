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

type Bucket struct {
	Timestamp int64
	Stats     map[string]Traffic
}

type Window struct {
	buckets  []*Bucket
	size     time.Duration
	bucketMu sync.RWMutex
	ipStats  map[string]Traffic
	statsMu  sync.RWMutex
	cache    []Traffic
	ipCache  []IPTraffic
	cacheMu  sync.RWMutex
	lastCalc time.Time
	Rank     int
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
		buckets: buckets,
		size:    size,
		ipStats: make(map[string]Traffic),
		Rank:    rank,
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
	w.statsMu.Unlock()
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

func (w *Window) StartCacheUpdate(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	ticker := time.NewTicker(2 * time.Second)
	for {
		select {
		case <-ticker.C:
			result := w.Summary()
			ipResult := w.IPSummary()
			w.cacheMu.Lock()
			w.cache = result
			w.ipCache = ipResult
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
