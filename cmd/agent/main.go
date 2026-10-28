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
	"strconv"
	"time"
)

func main() {
	port := flag.Int("port", 8080, "HTTP server port")
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

	addr := ":" + strconv.Itoa(*port)
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
