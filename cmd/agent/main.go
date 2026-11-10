package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"node-agent/internal/server"
	"node-agent/internal/specs"
	"os"
	"os/signal"
	"time"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "address to listen on (host:port)")
	name := flag.String("name", "", "Override reported hostname")
	flag.Parse()

	collect := func() (specs.Specs, error) {
		s, err := specs.Collect()
		if err != nil {
			return specs.Specs{}, err
		}

		if *name != "" {
			s.Hostname = *name
		}

		return s, nil
	}

	addr := *listen
	handler := server.NewHandler(collect)

	fmt.Printf("Agent starting on %s\n", addr)
	srv := &http.Server{
		Addr:    addr,
		Handler: handler,
	}
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
	)
	defer stop()
	go func() {
		if err := srv.ListenAndServe(); err != nil &&
			err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Graceful shutdown failed: %v", err)
	}
}
