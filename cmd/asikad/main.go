package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"asika/common/config"
	"asika/common/db"
	"asika/common/utils"
	"asika/common/version"
	"asika/daemon/server"
	"asika/daemon/server/core"
)

const (
	defaultShutdownTimeout = 30 * time.Second
	metricsLogInterval     = 5 * time.Minute
)

func main() {
	desktopMode := flag.Bool("desktop", false, "Run in desktop foreground mode (open browser to WebUI)")
	versionFlag := flag.Bool("version", false, "Print version and exit")
	shutdownTimeout := flag.Duration("shutdown-timeout", defaultShutdownTimeout, "Graceful shutdown timeout")
	metricsInterval := flag.Duration("metrics-interval", metricsLogInterval, "Metrics logging interval (0 to disable)")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("Asika daemon version %s\n", version.Version)
		return
	}

	configPath := os.Getenv("ASIKA_CONFIG")
	if configPath == "" {
		configPath = "/etc/asika_config.toml"
	}

	cfg, err := config.Load(configPath)

	if err != nil {
		slog.Warn("config not found, starting in initialization mode", "error", err)
		srv := server.NewServer(nil, nil)
		slog.Info("asikad starting in initialization mode", "version", version.Version)

		if *desktopMode {
			go func() {
				time.Sleep(500 * time.Millisecond)
				slog.Info("opening browser", "url", "http://localhost:8080")
				if err := utils.OpenBrowser("http://localhost:8080"); err != nil {
					slog.Warn("failed to open browser", "error", err)
				}
			}()
		}

		if err := srv.Start(); err != nil {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
		return
	}

	ic, err := core.Bootstrap(cfg)
	if err != nil {
		slog.Error("bootstrap failed", "error", err)
		os.Exit(1)
	}

	slog.Info("Asika daemon starting", "version", version.Version)
	slog.Info("Copyright (R) 2026 The Asika Project. All rights reserved.")

	if *metricsInterval > 0 {
		server.LogMetrics(*metricsInterval)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	if *desktopMode {
		listenAddr := ":8080"
		if cfg.Server.Listen != "" {
			listenAddr = cfg.Server.Listen
		}
		url := serverURL(listenAddr)
		go func() {
			time.Sleep(500 * time.Millisecond)
			slog.Info("opening browser", "url", url)
			if err := utils.OpenBrowser(url); err != nil {
				slog.Warn("failed to open browser", "error", err)
			}
		}()
	}

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- ic.Server.Start()
	}()

	sig := <-sigChan
	slog.Info("received signal, shutting down", "signal", sig, "timeout", *shutdownTimeout)

	server.SetHealth(false)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), *shutdownTimeout)
	defer shutdownCancel()

	go func() {
		if ic.QueueMgr != nil {
			ic.QueueMgr.Stop()
		}
		if ic.SpamDetector != nil {
			ic.SpamDetector.Stop()
		}
		if ic.EventConsumer != nil {
			ic.EventConsumer.Stop()
		}
		if ic.Poller != nil {
			ic.Poller.Stop()
		}

		if ic.TgBot != nil {
			ic.TgBot.Stop()
		}
		if ic.FsBot != nil {
			ic.FsBot.Stop()
		}
		if ic.DiscordBot != nil {
			ic.DiscordBot.Stop()
		}

		if err := ic.Server.Stop(); err != nil {
			slog.Error("server stop error", "error", err)
		}

		slog.Info("closing database")
		db.Close()
		slog.Info("database closed")

		slog.Info("shutdown complete")
		shutdownCancel()
	}()

	select {
	case <-shutdownCtx.Done():
		if shutdownCtx.Err() == context.DeadlineExceeded {
			slog.Warn("shutdown timed out, forcing exit")
		}
	case err := <-serverErrors:
		if err != nil && err != http.ErrServerClosed {
			slog.Error("server error during shutdown", "error", err)
		}
	}

	os.Exit(0)
}

func serverURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://localhost:8080"
	}
	if host == "" || host == "0.0.0.0" {
		host = "localhost"
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("http://%s:%s", host, port)
}
