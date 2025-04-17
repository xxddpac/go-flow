package main

import (
	"container/list"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
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

const (
	timeLayout = "2006-01-02 15:04:05"
	port       = 31415
)

var (
	ctx    context.Context
	cancel context.CancelFunc
)

func init() {
	ctx, cancel = context.WithCancel(context.Background())
}

type Logger struct{}

func (l Logger) Printf(format string, args ...interface{}) {
	zap.S().Infof(format, args...)
}

type Window struct {
	queue *list.List
	size  time.Duration
	mu    sync.Mutex
}

func NewWindow(size time.Duration) *Window {
	return &Window{
		queue: list.New(),
		size:  size,
	}
}

func (w *Window) Add(traffic Traffic) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.queue.PushBack(traffic)
	w.cleanup()
}

func (w *Window) cleanup() {
	now := time.Now()
	cutoff := now.Add(-w.size)
	for e := w.queue.Front(); e != nil; {
		stats := e.Value.(Traffic)
		if stats.Timestamp.Before(cutoff) {
			next := e.Next()
			w.queue.Remove(e)
			e = next
		} else {
			break
		}
	}
}

func (w *Window) Summary() []Traffic {
	w.mu.Lock()
	defer w.mu.Unlock()
	ipStats := make(map[string]Traffic)
	now := time.Now()
	cutoff := now.Add(-w.size)
	for e := w.queue.Front(); e != nil; e = e.Next() {
		stats := e.Value.(Traffic)
		if stats.Timestamp.Before(cutoff) {
			continue
		}
		s := ipStats[stats.IP]
		s.IP = stats.IP
		s.DstIP = stats.DstIP
		s.SrcPort = stats.SrcPort
		s.DstPort = stats.DstPort
		s.Protocol = stats.Protocol
		s.Bytes += stats.Bytes
		s.Requests += stats.Requests
		ipStats[stats.IP] = s
	}
	result := make([]Traffic, 0, len(ipStats))
	for _, s := range ipStats {
		mb := s.Bytes * 8 / 1e6
		if mb >= 1000 {
			s.Bytes = mb / 1000
			s.Unit = "Gb"
		} else {
			s.Bytes = mb
			s.Unit = "Mb"
		}
		result = append(result, s)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Bytes*getUnitFactor(result[i].Unit) > result[j].Bytes*getUnitFactor(result[j].Unit)
	})
	if len(result) > 10 {
		result = result[:10]
	}
	return result
}

func (w *Window) ServeHTTP(wr http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/top10" {
		top10 := w.Summary()
		response := make([]Top10Response, len(top10))
		for i, s := range top10 {
			response[i] = Top10Response{
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
		wr.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(wr).Encode(response); err != nil {
			http.Error(wr, "Failed to encode JSON", http.StatusInternalServerError)
		}
		return
	}
	http.ServeFile(wr, r, "index.html")
}

func getUnitFactor(unit string) float64 {
	if unit == "Gb" {
		return 1000
	}
	return 1
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
	handle, err := pcap.OpenLive(device, 1024, false, 30*time.Second)
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
				stats := Traffic{
					Timestamp: time.Now(),
					IP:        ipStr,
					DstIP:     dstIP,
					SrcPort:   srcPort,
					DstPort:   dstPort,
					Protocol:  protocol,
					Bytes:     float64(packetLen),
					Requests:  1,
				}
				w.Add(stats)
				return nil
			}, packet)
		case <-ticker.C:
			// todo send high bandwidth traffic alert
		case <-ctx.Done():
			return
		}
	}
}

type Top10Response struct {
	IP        string  `json:"ip"`
	DstIP     string  `json:"dstIP"`
	SrcPort   uint16  `json:"srcPort"`
	DstPort   uint16  `json:"dstPort"`
	Protocol  string  `json:"protocol"`
	Bandwidth float64 `json:"bandwidth"`
	Unit      string  `json:"unit"`
	Requests  int64   `json:"requests"`
}

func main() {
	eth := flag.String("eth", "", "Network interface")
	size := flag.Int("size", 5, "Sliding window size")
	flag.Parse()

	if *eth == "" {
		iFaces, _ := pcap.FindAllDevs()
		_, _ = fmt.Fprintln(os.Stderr, "Available interfaces:")
		for _, iFace := range iFaces {
			_, _ = fmt.Fprintf(os.Stderr, "- %s: %s\n", iFace.Name, iFace.Description)
		}
	}

	if *size <= 0 {
		_, _ = fmt.Fprintln(os.Stderr, "Error: --size must be positive")
		flag.Usage()
		os.Exit(1)
	}
	var (
		config = zap.NewProductionConfig()
		pool   = async.New(async.WithLogger(Logger{}))
		window = NewWindow(time.Duration(*size) * time.Minute)
	)
	config.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(timeLayout)
	cb, _ := config.Build()
	zap.ReplaceGlobals(cb)
	defer func() {
		pool.Logger.Printf("+++++ quit +++++")
		pool.Close()
		_ = cb.Sync()
	}()
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: nil,
	}
	http.Handle("/", window)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	pool.Wg.Add(2)
	go capture(ctx, *eth, pool, window)
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
