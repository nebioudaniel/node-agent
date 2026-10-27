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

	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("HTTP server failed: %v", err)
	}
}
