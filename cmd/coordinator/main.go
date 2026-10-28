package main

import (
	"encoding/json"
	"log"
	"net/http"
)

type Heartbeat struct {
	WorkerID string `json:"worker_id"`
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Cores    int    `json:"cores"`
	TotalRAM uint64 `json:"total_ram"`
}

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var hb Heartbeat
		if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		if hb.WorkerID == "" || hb.Hostname == "" {
			http.Error(w, "worker_id and hostname are required", http.StatusBadRequest)
			return
		}

		log.Printf("heartbeat received from worker %s (%s)", hb.WorkerID, hb.Hostname)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "received",
		})
	})

	log.Println("coordinator listening on :8081")
	log.Fatal(http.ListenAndServe(":8081", mux))
}
