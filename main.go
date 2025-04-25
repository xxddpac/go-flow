package main

import (
	"container/heap"
	"context"
	"embed"
	"flag"
	"fmt"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	ji "github.com/json-iterator/go"
	"github.com/xxddpac/async"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"hash/fnv"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"
)

//go:embed index.html
//go:embed static/*
var content embed.FS

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

type Logger struct{}

func (l Logger) Printf(format string, args ...interface{}) {
	zap.S().Infof(format, args...)
}

type Window struct {
	shards   []*Shard
	size     time.Duration
	cache    []Traffic
	cacheMu  sync.RWMutex
	lastCalc time.Time
	Rank     int
}

type Shard struct {
	mu      sync.Mutex
	slices  map[int64]map[string]Traffic
	ipStats map[string]Traffic
	last    time.Time
}

func NewWindow(size time.Duration, shardCount, rank int) *Window {
	w := &Window{
		shards: make([]*Shard, shardCount),
		size:   size,
		Rank:   rank,
	}
	for i := range w.shards {
		w.shards[i] = &Shard{
			slices:  make(map[int64]map[string]Traffic),
			ipStats: make(map[string]Traffic),
			last:    time.Now(),
		}
	}
	return w
}

func (w *Window) Add(traffic Traffic) {
	idx := fnv32(traffic.IP) % uint32(len(w.shards))
	shard := w.shards[idx]
	shard.mu.Lock()
	defer shard.mu.Unlock()

	ts := traffic.Timestamp.Unix()
	if _, ok := shard.slices[ts]; !ok {
		shard.slices[ts] = make(map[string]Traffic)
	}
	s := shard.slices[ts][traffic.IP]
	s.IP = traffic.IP
	s.DstIP = traffic.DstIP
	s.SrcPort = traffic.SrcPort
	s.DstPort = traffic.DstPort
	s.Protocol = traffic.Protocol
	s.Bytes += traffic.Bytes
	s.Requests += traffic.Requests
	shard.slices[ts][traffic.IP] = s

	agg := shard.ipStats[traffic.IP]
	agg.IP = traffic.IP
	agg.DstIP = traffic.DstIP
	agg.SrcPort = traffic.SrcPort
	agg.DstPort = traffic.DstPort
	agg.Protocol = traffic.Protocol
	agg.Bytes += traffic.Bytes
	agg.Requests += traffic.Requests
	shard.ipStats[traffic.IP] = agg
}

func (w *Window) cleanup(shard *Shard) {
	shard.mu.Lock()
	defer shard.mu.Unlock()
	cutoff := time.Now().Add(-w.size).Unix()
	for ts := range shard.slices {
		if ts < cutoff {
			for ip := range shard.slices[ts] {
				agg := shard.ipStats[ip]
				agg.Bytes -= shard.slices[ts][ip].Bytes
				agg.Requests -= shard.slices[ts][ip].Requests
				if agg.Bytes <= 0 {
					delete(shard.ipStats, ip)
				} else {
					shard.ipStats[ip] = agg
				}
			}
			delete(shard.slices, ts)
		}
	}
	shard.last = time.Now()
}

func (w *Window) StartCleanup(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	ticker := time.NewTicker(time.Second)
	for {
		select {
		case <-ticker.C:
			for _, shard := range w.shards {
				if time.Since(shard.last) > time.Second {
					w.cleanup(shard)
				}
			}
		case <-ctx.Done():
			ticker.Stop()
			return
		}
	}
}

func (w *Window) StartCacheUpdate(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	for {
		select {
		case <-ticker.C:
			result := w.Summary()
			w.cacheMu.Lock()
			w.cache = result
			w.lastCalc = time.Now()
			w.cacheMu.Unlock()
		case <-ctx.Done():
			ticker.Stop()
			return
		}
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

func (w *Window) Summary() []Traffic {
	ipStats := make(map[string]Traffic)
	for _, shard := range w.shards {
		shard.mu.Lock()
		for ip, s := range shard.ipStats {
			agg := ipStats[ip]
			agg.IP = s.IP
			agg.DstIP = s.DstIP
			agg.SrcPort = s.SrcPort
			agg.DstPort = s.DstPort
			agg.Protocol = s.Protocol
			agg.Bytes += s.Bytes
			agg.Requests += s.Requests
			ipStats[ip] = agg
		}
		shard.mu.Unlock()
	}

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

func (w *Window) ServeHTTP(wr http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/top" {
		w.cacheMu.RLock()
		top := w.cache
		w.cacheMu.RUnlock()
		resp := make([]TopItem, len(top))
		for i, s := range top {
			resp[i] = TopItem{
				IP:        s.IP,
				DstIP:     s.DstIP,
				SrcPort:   s.SrcPort,
				DstPort:   s.DstPort,
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

func getUnitFactor(unit string) float64 {
	switch unit {
	case "GB":
		return 1000
	case "MB":
		return 1
	default:
		return 1
	}
}

func fnv32(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}

type Traffic struct {
	Timestamp time.Time
	IP        string
	DstIP     string
	SrcPort   uint16
	DstPort   uint16
	Protocol  string
	Bytes     float64
	Requests  int64
	Unit      string
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
				var srcPort, dstPort uint16
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
					srcPort = uint16(tcp.SrcPort)
					dstPort = uint16(tcp.DstPort)
				} else if udpLayer := pt.Layer(layers.LayerTypeUDP); udpLayer != nil {
					udp, _ := udpLayer.(*layers.UDP)
					protocol = "UDP"
					srcPort = uint16(udp.SrcPort)
					dstPort = uint16(udp.DstPort)
				} else {
					return nil
				}
				packetLen := int64(pt.Metadata().Length)
				stats := syncPool.Get().(*Traffic)
				*stats = Traffic{
					Timestamp: time.Now(),
					IP:        ipStr,
					DstIP:     dstIP,
					SrcPort:   srcPort,
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
		case <-ctx.Done():
			return
		}
	}
}

type TopItem struct {
	IP        string  `json:"IP"`
	DstIP     string  `json:"DstIP"`
	SrcPort   uint16  `json:"SrcPort"`
	DstPort   uint16  `json:"DstPort"`
	Protocol  string  `json:"Protocol"`
	Bandwidth float64 `json:"Bandwidth"`
	Unit      string  `json:"Unit"`
	Requests  int64   `json:"Requests"`
}

type TopResponse struct {
	Data       []TopItem `json:"Data"`
	Timestamp  int64     `json:"Timestamp"`
	WindowSize int       `json:"WindowSize"`
	Rank       int       `json:"Rank"`
}

func main() {
	eth := flag.String("eth", "", "Network interface")
	size := flag.Int("size", 5, "Sliding window size")
	workers := flag.Int("workers", 1000, "Number of workers")
	rank := flag.Int("rank", 10, "Top Rank")
	flag.Parse()

	if *eth == "" {
		iFaces, _ := pcap.FindAllDevs()
		_, _ = fmt.Fprintln(os.Stderr, "Available interfaces:")
		for _, iFace := range iFaces {
			_, _ = fmt.Fprintf(os.Stderr, "- %s: %s\n", iFace.Name, iFace.Description)
		}
		os.Exit(1)
	}

	if *size <= 0 || *rank <= 0 || *workers <= 0 {
		_, _ = fmt.Fprintln(os.Stderr, "Error: --size must be positive")
		flag.Usage()
		os.Exit(1)
	}
	var (
		config = zap.NewProductionConfig()
		pool   = async.New(
			async.WithMaxWorkers(*workers),
			async.WithMaxQueue(*workers*10),
			async.WithLogger(Logger{}),
		)
		window = NewWindow(time.Duration(*size)*time.Minute, 16, *rank)
	)
	config.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(timeLayout)
	cb, _ := config.Build()
	zap.ReplaceGlobals(cb)
	defer func() {
		pool.Logger.Printf("+++++ quit +++++")
		pool.Close()
		_ = cb.Sync()
	}()

	mux := http.NewServeMux()
	mux.Handle("/", window)
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	pool.Wg.Add(4)
	go window.StartCleanup(ctx, &pool.Wg)
	go window.StartCacheUpdate(ctx, &pool.Wg)
	go capture(ctx, *eth, pool, window)
	go func() {
		if err := http.ListenAndServe(fmt.Sprintf(":%d", port-1), nil); err != nil {
			pool.Logger.Printf("pprof: %s", fmt.Sprintf("pprof err: %v", err))
		}
	}()
	go func() {
		defer pool.Wg.Done()
		pool.Logger.Printf(fmt.Sprintf("HTTP server starting at port:%d", port))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			pool.Logger.Printf("HTTP server error: %v", err)
		}
	}()
	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt, os.Kill, syscall.SIGKILL,
			syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGILL, syscall.SIGTRAP,
			syscall.SIGABRT,
		)
		select {
		case sig := <-signals:
			pool.Logger.Printf("Received signal: %v", sig.String())
			_ = server.Shutdown(ctx)
			<-time.After(time.Second)
			cancel()
		}
	}()
	pool.Wg.Wait()
}
