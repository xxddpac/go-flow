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
	"go-flow/zlog"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	var cfg string
	flag.StringVar(&cfg, "c", "", "config.toml")
	flag.Parse()
	if len(cfg) == 0 {
		fmt.Println("config is empty")
		os.Exit(0)
	}
	conf.Init(cfg)
	zlog.Init(zlog.NewZLog(&conf.CoreConf.Log))
	var (
		pool = async.New(
			async.WithMaxWorkers(conf.CoreConf.Server.Workers),
			async.WithMaxQueue(conf.CoreConf.Server.Workers*10),
		)
		window = flow.NewWindow(time.Duration(conf.CoreConf.Server.Size)*time.Minute, conf.CoreConf.Server.Rank)
	)
	defer pool.Close()

	mux := http.NewServeMux()
	mux.Handle("/", window)
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", conf.CoreConf.Server.Port),
		Handler: mux,
	}
	if conf.CoreConf.Kafka.Enable {
		if err := kafka.Init(&conf.CoreConf.Kafka); err != nil {
			log.Fatalf("Init kafka failed: %v", err)
		}
	}
	pool.Wg.Add(4)
	go notify.Init(&pool.Wg)
	go window.StartCacheUpdate(&pool.Wg)
	go flow.Capture(conf.CoreConf.Server.Eth, pool, window)
	go func() {
		if err := http.ListenAndServe(fmt.Sprintf(":%d", conf.CoreConf.Server.Port-1), nil); err != nil {
			zlog.Errorf("Main", "pprof ListenAndServe Error %s", err.Error())
		}
	}()
	go func() {
		defer pool.Wg.Done()
		zlog.Infof("Main", "Starting HTTP server on http://localhost%s", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			zlog.Errorf("Main", "HTTP server ListenAndServe Error %s", err.Error())
		}
	}()
	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
		select {
		case sig := <-signals:
			pool.Logger.Printf("Received signal: %v", sig.String())
			_ = server.Shutdown(utils.Ctx)
			<-time.After(time.Second)
			utils.Cancel()
			kafka.Close()
			zlog.Close()
		}
	}()
	pool.Wg.Wait()
	pool.Logger.Printf("+++++ Bye +++++")
}
