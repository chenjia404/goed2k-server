package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chenjia404/goed2k-server/ed2ksrv"
)

func main() {
	configPath := flag.String("config", "config.json", "Path to the server JSON config file")
	flag.Parse()

	cfg, usedDefaults, err := ed2ksrv.LoadConfig(*configPath)
	if err != nil {
		slog.Error("load config failed", "path", *configPath, "err", err)
		os.Exit(1)
	}
	if usedDefaults {
		slog.Info("config file not found, using built-in defaults", "path", *configPath)
	}

	server, err := ed2ksrv.NewServer(cfg, nil, slog.Default())
	if err != nil {
		slog.Error("create server failed", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown failed", "err", err)
		}
	}()

	slog.Info("server starting", "listen", cfg.ListenAddress, "storage_backend", cfg.StorageBackend, "catalog", cfg.CatalogPath, "database_table", cfg.DatabaseTable)
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
