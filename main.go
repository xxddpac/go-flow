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
	w.bucketMu.Lock()
	now := time.Now()
	idx := int(now.Unix()/60) % len(w.buckets)
	bucket := w.buckets[idx]
	bucketTs := now.Truncate(time.Minute).Unix()
	if bucket.Timestamp != bucketTs {
		bucket.Timestamp = bucketTs
		bucket.Stats = make(map[string]Traffic)
		w.statsMu.Lock()
		for ip := range bucket.Stats {
			if stat, ok := w.ipStats[ip]; ok {
				stat.Bytes -= bucket.Stats[ip].Bytes
				stat.Requests -= bucket.Stats[ip].Requests
				if stat.Bytes <= 0 {
					delete(w.ipStats, ip)
				} else {
					w.ipStats[ip] = stat
				}
			}
		}
		w.statsMu.Unlock()
	}
	s := bucket.Stats[traffic.IP]
	s.IP = traffic.IP
	s.DstIP = traffic.DstIP
	s.SrcPort = traffic.SrcPort
	s.DstPort = traffic.DstPort
	s.Protocol = traffic.Protocol
	s.Bytes += traffic.Bytes
	s.Requests += traffic.Requests
	bucket.Stats[traffic.IP] = s
	w.bucketMu.Unlock()

	w.statsMu.Lock()
	agg := w.ipStats[traffic.IP]
	agg.IP = traffic.IP
	agg.DstIP = traffic.DstIP
	agg.SrcPort = traffic.SrcPort
	agg.DstPort = traffic.DstPort
	agg.Protocol = traffic.Protocol
	agg.Bytes += traffic.Bytes
	agg.Requests += traffic.Requests
	w.ipStats[traffic.IP] = agg
	w.statsMu.Unlock()
}

func (w *Window) Summary() []Traffic {
	w.statsMu.RLock()
	ipStats := make(map[string]Traffic, len(w.ipStats))
	for ip, s := range w.ipStats {
		if s.Bytes > 0 {
			ipStats[ip] = s
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
					Timestamp: pt.Metadata().Timestamp,
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
			//todo high bandwidth alert
			//for _,item := range w.cache {
			//	if item.Bytes > 1000 {
			//		pool.Logger.Printf("High bandwidth alert: %s %s %s %s %s", item.IP, item.DstIP, item.SrcPort, item.DstPort, item.Protocol)
			//	}
			//}
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
		window = NewWindow(time.Duration(*size)*time.Minute, *rank)
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

	pool.Wg.Add(3)
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
