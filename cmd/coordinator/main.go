package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"node-agent/internal/coordinator"
	"node-agent/internal/coordinator/registry"
)

func main() {
	addr := flag.String("addr", ":9090", "address to listen on")
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	flag.Parse()

	reg := registry.New(nil)

	srv := &http.Server{
		Addr:    *addr,
		Handler: coordinator.NewHandler(reg),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Sweeper logs worker status transitions (online->stale->offline, etc.)
	// every 5s. It does not set status; status is computed on read. Clean
	// shutdown: sweeper returns when ctx is cancelled.
	go registry.RunSweeper(ctx, reg, 5*time.Second, slog.Default())

	go func() {
		slog.Info("coordinator listening", "addr", *addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("coordinator shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "err", err)
	}
	slog.Info("coordinaor stopped")
}
