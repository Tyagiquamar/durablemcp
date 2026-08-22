package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/tyagiquamar/durablemcp/internal/config"
	"github.com/tyagiquamar/durablemcp/internal/logging"
	"github.com/tyagiquamar/durablemcp/internal/store"
)

func main() {
	cfg := config.Load()
	logger := logging.New(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.NewPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect postgres: %v", err)
	}
	defer st.Close()

	interval := time.Duration(cfg.TickSeconds) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Info("durablemcp scheduler started", "tick", interval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			changed, err := st.RunSchedulerPass(ctx)
			if err != nil {
				logger.Error("scheduler pass failed", "err", err)
				continue
			}
			if changed > 0 {
				logger.Info("scheduler pass", "changed", changed)
			}
		}
	}
}
