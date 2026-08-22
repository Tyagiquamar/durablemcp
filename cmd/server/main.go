package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/tyagiquamar/durablemcp/internal/api"
	"github.com/tyagiquamar/durablemcp/internal/config"
	"github.com/tyagiquamar/durablemcp/internal/executor"
	"github.com/tyagiquamar/durablemcp/internal/logging"
	"github.com/tyagiquamar/durablemcp/internal/mcp"
	"github.com/tyagiquamar/durablemcp/internal/store"
)

const version = "0.1.0"

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

	if err := st.SeedTools(ctx, executor.DemoTools()); err != nil {
		log.Fatalf("seed tools: %v", err)
	}

	handler := &mcp.Handler{Store: st, ServerName: "durablemcp", Version: version}

	if cfg.MCPTransport == "stdio" {
		logger.Info("durablemcp mcp server ready (stdio)")
		if err := handler.ServeStdio(ctx, os.Stdin, os.Stdout); err != nil {
			log.Fatalf("stdio transport: %v", err)
		}
		return
	}

	allowTest := os.Getenv("ALLOW_TEST_ENDPOINTS") == "true"
	srv := api.New(st, cfg.ReaderAPIKey, allowTest, handler.HTTPHandler(), handler.SSEHandler())

	httpServer := &http.Server{Addr: cfg.Addr, Handler: srv}
	go func() {
		<-ctx.Done()
		_ = httpServer.Close()
	}()

	logger.Info("durablemcp server listening", "addr", cfg.Addr, "transports", "mcp-http + rest")
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("http server: %v", err)
	}
}
