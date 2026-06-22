package config

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	AdvertiseIP string
	SIPPort     int
	RTPPort     int
	HTTPAddr    string
}

func Load() (Config, error) {
	loadDotEnv(".env")

	cfg := Config{
		AdvertiseIP: os.Getenv("ADVERTISE_IP"),
		SIPPort:     intValue("SIP_LOCAL_PORT", 5066),
		RTPPort:     intValue("RTP_PORT", 40000),
		HTTPAddr:    value("HTTP_ADDR", "127.0.0.1:8080"),
	}
	if cfg.AdvertiseIP == "" {
		ip, err := localIPv4()
		if err != nil {
			return Config{}, fmt.Errorf("ADVERTISE_IP is required: %w", err)
		}
		cfg.AdvertiseIP = ip
	}
	if net.ParseIP(cfg.AdvertiseIP) == nil {
		return Config{}, fmt.Errorf("invalid ADVERTISE_IP %q", cfg.AdvertiseIP)
	}
	return cfg, nil
}

func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if ok {
			key = strings.TrimSpace(key)
			if os.Getenv(key) == "" {
				_ = os.Setenv(key, strings.TrimSpace(val))
			}
		}
	}
}

func value(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func intValue(key string, fallback int) int {
	n, err := strconv.Atoi(os.Getenv(key))
	if err != nil {
		return fallback
	}
	return n
}

func localIPv4() (string, error) {
	conn, err := net.Dial("udp4", "8.8.8.8:53")
	if err != nil {
		return "", err
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String(), nil
}
