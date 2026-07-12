package headless

import (
	"net"
	"os"
	"os/signal"
	"strconv"

	"log/slog"

	"cipherproxy/internal/config"
	"cipherproxy/internal/log"
	"cipherproxy/internal/tunnel"
)

func Run() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}
	mode := tunnel.FastReconnect
	if cfg.ResilienceMode {
		mode = tunnel.NetworkResilience
	}

	logger := log.NewLogger(func(line string) { os.Stdout.WriteString(line + "\n") })

	statusFn := func(s tunnel.Status) {
		switch s {
		case tunnel.StatusConnected:
			logger.Info("status: CONNECTED")
		case tunnel.StatusRetrying:
			logger.Info("status: RETRYING")
		case tunnel.StatusDisconnected:
			logger.Info("status: DISCONNECTED")
		}
	}

	addr := net.JoinHostPort(cfg.Address, strconv.Itoa(cfg.Port))
	engine := tunnel.NewEngine(addr, cfg.User, cfg.Password, mode, logger, statusFn)
	router := tunnel.NewRouter(cfg.BlackListEntries())

	stopCh := make(chan struct{})
	go engine.Run(stopCh)

	if _, err := tunnel.StartSocksServer(cfg.SocksProxyPort, engine, router, logger); err != nil {
		logger.Error("failed to start SOCKS server", "err", err)
		os.Exit(1)
	}
	if _, err := tunnel.StartHTTPProxyServer(cfg.HTTPProxyPort, engine, router, logger); err != nil {
		logger.Error("failed to start HTTP proxy server", "err", err)
		os.Exit(1)
	}

	logger.Info("Cipher Proxy (headless) running. Press Ctrl+C to stop.")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
	close(stopCh)
}
