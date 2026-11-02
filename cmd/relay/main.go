package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/mviruch/gb28181-relay/internal/config"
	"github.com/mviruch/gb28181-relay/internal/sipua"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to YAML config")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Error("load config", "err", err)
		os.Exit(1)
	}

	rt, err := sipua.NewRuntime(cfg, log)
	if err != nil {
		log.Error("init sip runtime", "err", err)
		os.Exit(1)
	}
	defer rt.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Info("gb28181-relay starting",
		"listen", cfg.ListenAddr(),
		"publicHost", cfg.Listen.PublicHost,
		"devices", len(cfg.Devices),
		"zlm", cfg.ZLM.API,
	)
	if err := rt.Start(ctx); err != nil && err != context.Canceled {
		log.Error("runtime stopped", "err", err)
		os.Exit(1)
	}
	log.Info("gb28181-relay stopped")
}
