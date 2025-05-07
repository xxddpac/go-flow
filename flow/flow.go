package flow

import (
	"container/heap"
	"context"
	"embed"
	"fmt"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	ji "github.com/json-iterator/go"
	"github.com/xxddpac/async"
	"go-flow/conf"
	"go-flow/notify"
	"go-flow/utils"
	"net/http"
	"sort"
	"sync"
	"time"
)

var syncPool = sync.Pool{New: func() interface{} { return &Traffic{} }}

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

func Capture(ctx context.Context, device string, pool *async.WorkerPool, w *Window) {
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
			//【alert】 high bandwidth for single ip
			var alerts []notify.Ddos
			thresholdBytes := getThresholdBytes()
			w.cacheMu.RLock()
			for _, s := range w.ipCache {
				bytes := s.Bytes * getUnitFactor(s.Unit)
				if bytes > thresholdBytes {
					if notify.IsWhiteIp(s.IP) {
						continue
					}
					alerts = append(alerts, notify.Ddos{
						IP:        s.IP,
						Bandwidth: fmt.Sprintf("%.2f%s", s.Bytes, s.Unit),
					})
				}
			}
			if len(alerts) > 0 {
				notify.Base.Queue(notify.DdosAlert{
					Alerts:    alerts,
					Timestamp: time.Now().Format(utils.TimeLayout),
					Location:  conf.CoreConf.Notify.Location,
					TimeRange: utils.GetTimeRangeString(conf.CoreConf.Server.Size),
				})
			}
			w.cacheMu.RUnlock()
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
	Data       []TopItem `json:"data"`
	Timestamp  int64     `json:"timestamp"`
	WindowSize int       `json:"window_size"`
	Rank       int       `json:"rank"`
}

type PortResponse struct {
	DstPort  string  `json:"dest_port"`
	Bytes    float64 `json:"bytes"`
	Protocol string  `json:"protocol"`
	Unit     string  `json:"unit"`
}

type TrendResponse struct {
	Data       []TrendItem `json:"data"`
	WindowSize int         `json:"window_size"`
}

func (w *Window) ServeHTTP(wr http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/top" {
		w.cacheMu.RLock()
		top := w.cache
		w.cacheMu.RUnlock()
		resp := make([]TopItem, len(top))
		for i, s := range top {
			var (
				ok      bool
				service string
				portStr = fmt.Sprintf("%d", s.DstPort)
			)
			service, ok = utils.PortMapping[portStr]
			if !ok {
				service = portStr
			} else {
				service = fmt.Sprintf("%s / %s", portStr, service)
			}
			resp[i] = TopItem{
				SrcIP:     s.SrcIP,
				DstIP:     s.DstIP,
				DstPort:   service,
				Protocol:  s.Protocol,
				Bandwidth: s.Bytes,
				Unit:      s.Unit,
				Requests:  s.Requests,
			}
		}
		response := TopResponse{
			Data:       resp,
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
		w.cacheMu.RLock()
		top := w.ipCache
		w.cacheMu.RUnlock()
		resp := make([]IPTraffic, len(top))
		for i, s := range top {
			resp[i] = IPTraffic{
				IP:       s.IP,
				Unit:     s.Unit,
				Requests: s.Requests,
				Bytes:    s.Bytes,
			}
		}
		wr.Header().Set("Content-Type", "application/json")
		if err := ji.NewEncoder(wr).Encode(resp); err != nil {
			http.Error(wr, "Failed to encode JSON", http.StatusInternalServerError)
		}
		return
	}
	if r.URL.Path == "/stats/ports" {
		w.cacheMu.RLock()
		top := w.portCache
		w.cacheMu.RUnlock()
		response := make([]PortResponse, len(top))
		for i, s := range top {
			var (
				ok      bool
				dPort   string
				portStr = fmt.Sprintf("%d", s.DstPort)
			)
			dPort, ok = utils.PortMapping[portStr]
			if !ok {
				dPort = portStr
			} else {
				dPort = fmt.Sprintf("%s / %s", portStr, dPort)
			}
			response[i] = PortResponse{
				DstPort:  dPort,
				Bytes:    s.Bytes,
				Protocol: s.Protocol,
				Unit:     s.Unit,
			}
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
