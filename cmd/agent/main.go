package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"node-agent/internal/server"
	"node-agent/internal/specs"
)

func main() {
	port := flag.Int("port", 8080, "HTTP server port")
	name := flag.String("name", "", "Override reported hostname")
	flag.Parse()

	s, err := specs.Collect()
	if err != nil {
		log.Fatalf("collect system specs: %v", err)
	}

	if *name != "" {
		s.Hostname = *name
	}

	addr := ":" + strconv.Itoa(*port)
	handler := server.NewHandler(s)

	fmt.Printf("Agent %q starting on %s\n", s.Hostname, addr)

	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("HTTP server failed: %v", err)
	}
}
