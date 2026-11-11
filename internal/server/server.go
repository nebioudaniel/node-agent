package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"node-agent/internal/specs"
)

func NewHandler(collect func() (specs.Specs, error)) http.Handler {

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	mux.HandleFunc("/specs", func(w http.ResponseWriter, r *http.Request) {
		s, err := collect()
		if err != nil {
			slog.Error("collect specs faild", "err", err)

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)

			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "failed to collect system specs",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(s); err != nil {
			slog.Error("encode specs repsonse failed", "err", err)
		}
	})

	return mux
}
