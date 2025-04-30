package main

import (
	"embed"
	"fmt"
	ji "github.com/json-iterator/go"
	"net/http"
)

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
			service, ok = portMapping[portStr]
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
			dPort, ok = portMapping[portStr]
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
