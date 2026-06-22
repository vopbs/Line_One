package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"webrtc-sip/internal/config"
	"webrtc-sip/internal/gateway"
)

func main() {
	setupLogFile()
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	gw, err := gateway.New(cfg)
	if err != nil {
		if isPortInUse(err) {
			url := browserURL(cfg.HTTPAddr)
			log.Printf("Gateway already appears to be running, opening %s", url)
			if openErr := openBrowser(url); openErr != nil {
				log.Printf("open browser error: %v", openErr)
			}
			return
		}
		log.Fatal(err)
	}
	log.Printf("Web phone: http://%s", cfg.HTTPAddr)
	openBrowserSoon(browserURL(cfg.HTTPAddr))
	server := &http.Server{Addr: cfg.HTTPAddr}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/shutdown", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		go func() {
			time.Sleep(200 * time.Millisecond)
			gw.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := server.Shutdown(ctx); err != nil {
				log.Printf("HTTP shutdown error: %v", err)
			}
		}()
	})
	mux.Handle("/", gw.Routes())
	server.Handler = mux
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func setupLogFile() {
	file, err := os.OpenFile("webrtc-sip.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		log.SetOutput(file)
	}
}

func openBrowserSoon(url string) {
	if os.Getenv("OPEN_BROWSER") == "0" {
		return
	}
	go func() {
		time.Sleep(700 * time.Millisecond)
		if err := openBrowser(url); err != nil {
			log.Printf("open browser error: %v", err)
		}
	}()
}

func browserURL(httpAddr string) string {
	hostPort := httpAddr
	if strings.HasPrefix(hostPort, ":") {
		hostPort = "127.0.0.1" + hostPort
	}
	if strings.HasPrefix(hostPort, "0.0.0.0:") {
		hostPort = "127.0.0.1:" + strings.TrimPrefix(hostPort, "0.0.0.0:")
	}
	return "http://" + hostPort
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

func isPortInUse(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return strings.Contains(strings.ToLower(opErr.Err.Error()), "address already in use") ||
			strings.Contains(strings.ToLower(opErr.Err.Error()), "only one usage of each socket address")
	}
	return strings.Contains(strings.ToLower(fmt.Sprint(err)), "address already in use") ||
		strings.Contains(strings.ToLower(fmt.Sprint(err)), "only one usage of each socket address")
}
