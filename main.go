package main

import (
	"flag"
	"fmt"
	"github.com/xxddpac/async"
	"go-flow/conf"
	"go-flow/flow"
	"go-flow/kafka"
	"go-flow/notify"
	"go-flow/utils"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type Logger struct{}

func (l Logger) Printf(format string, args ...interface{}) {
	zap.S().Infof(format, args...)
}

func main() {
	var cfg string
	flag.StringVar(&cfg, "c", "", "server config [toml]")
	flag.Parse()
	if len(cfg) == 0 {
		fmt.Println("config is empty")
		os.Exit(0)
	}
	conf.Init(cfg)
	var (
		config = zap.NewProductionConfig()
		pool   = async.New(
			async.WithMaxWorkers(conf.CoreConf.Server.Workers),
			async.WithMaxQueue(conf.CoreConf.Server.Workers*10),
			async.WithLogger(Logger{}),
		)
		window = flow.NewWindow(time.Duration(conf.CoreConf.Server.Size)*time.Minute, conf.CoreConf.Server.Rank)
	)
	config.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(utils.TimeLayout)
	cb, _ := config.Build()
	zap.ReplaceGlobals(cb)
	defer func() {
		pool.Logger.Printf("+++++ Bye +++++")
		pool.Close()
		_ = cb.Sync()
	}()

	mux := http.NewServeMux()
	mux.Handle("/", window)
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", conf.CoreConf.Server.Port),
		Handler: mux,
	}
	if conf.CoreConf.Kafka.Enable {
		kc := &kafka.Config{Brokers: conf.CoreConf.Kafka.Brokers, Topic: conf.CoreConf.Kafka.Topic, P: pool, Ctx: utils.Ctx}
		if err := kafka.Init(kc); err != nil {
			panic(fmt.Sprintf("Init kafka failed: %v", err))
		}
	}
	pool.Wg.Add(4)
	go notify.Init(utils.Ctx, pool)
	go window.StartCacheUpdate(utils.Ctx, &pool.Wg)
	go flow.Capture(utils.Ctx, conf.CoreConf.Server.Eth, pool, window)
	go func() {
		if err := http.ListenAndServe(fmt.Sprintf(":%d", conf.CoreConf.Server.Port-1), nil); err != nil {
			pool.Logger.Printf("pprof: %s", fmt.Sprintf("pprof err: %v", err))
		}
	}()
	go func() {
		defer pool.Wg.Done()
		pool.Logger.Printf(fmt.Sprintf("visit http://localhost:%d", conf.CoreConf.Server.Port))
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
			_ = server.Shutdown(utils.Ctx)
			<-time.After(time.Second)
			utils.Cancel()
			if conf.CoreConf.Kafka.Enable {
				kafka.Close()
			}
		}
	}()
	pool.Wg.Wait()
}
