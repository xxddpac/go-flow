package main

import (
	"embed"
	"flag"
	"fmt"
	"github.com/google/gopacket/pcap"
	"github.com/xxddpac/async"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

//go:embed index.html
//go:embed static/*
var content embed.FS

type Logger struct{}

func (l Logger) Printf(format string, args ...interface{}) {
	zap.S().Infof(format, args...)
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
