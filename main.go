package main

import (
	"flag"
	"fmt"
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
	"sync"
	"syscall"
	"time"
)

func main() {
	var (
		cfg string
		wg  sync.WaitGroup
	)
	flag.StringVar(&cfg, "c", "", "config.toml")
	flag.Parse()
	if len(cfg) == 0 {
		fmt.Println("config is empty")
		os.Exit(0)
	}
	conf.Init(cfg)
	zlog.Init(zlog.NewZLog(&conf.CoreConf.Log))
	window := flow.NewWindow(time.Duration(conf.CoreConf.Server.Size)*time.Minute, conf.CoreConf.Server.Rank)
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
	wg.Add(3)
	go notify.Init(&wg)
	go window.StartCacheUpdate(&wg)
	go flow.Capture(conf.CoreConf.Server.Eth, window, &wg)
	go func() {
		pprofPort := conf.CoreConf.Server.Port - 1
		zlog.Infof("Main", "Starting pprof on http://localhost:%d/debug/pprof/", pprofPort)
		if err := http.ListenAndServe(fmt.Sprintf(":%d", pprofPort), nil); err != nil {
			zlog.Errorf("Main", "pprof ListenAndServe Error %s", err.Error())
		}
	}()
	go func() {
		log.Printf("Starting HTTP server on http://localhost%s\n", server.Addr)
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
			log.Printf("Received signal: %s\n", sig)
			<-time.After(time.Second)
			_ = server.Shutdown(utils.Ctx)
			utils.Cancel()
			kafka.Close()
			zlog.Close()
		}
	}()
	wg.Wait()
	log.Println("+++++ Bye +++++")
}
