package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/tyagiquamar/durablemcp/internal/config"
	"github.com/tyagiquamar/durablemcp/internal/executor"
	"github.com/tyagiquamar/durablemcp/internal/store"
)

func main() {
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.NewPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect postgres: %v", err)
	}
	defer st.Close()

	scratch := os.Getenv("SCRATCH_DIR")
	if scratch == "" {
		scratch = os.TempDir()
	}

	runtime := &executor.Runtime{
		Store:        st,
		WorkerID:     cfg.WorkerID,
		Handlers:     executor.Handlers(scratch),
		PollInterval: cfg.PollInterval,
		LeaseSeconds: cfg.LeaseSeconds,
		Logger:       log.Default(),
	}

	if err := runtime.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("executor: %v", err)
	}
}
