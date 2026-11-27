package coordinator

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"node-agent/internal/api"
	"node-agent/internal/coordinator/registry"
)

// Server holds the dependencies for coordinator HTTP handlers.
type Server struct {
	reg *registry.Registry
	log *slog.Logger
}

// NewHandler builds the coordinator HTTP mux with the heartbeat and register routes.
// The registry is injected so it can be shared and tested independently.
func NewHandler(reg *registry.Registry) http.Handler {
	s := &Server{
		reg: reg,
		log: slog.Default(),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/workers/{id}/heartbeat", handlerHeartbeat)
	mux.HandleFunc("POST /v1/workers/register", s.handleRegister)
	return mux
}

// newWorkerID returns "wrk_" followed by 6 random bytes encoded as hex (12 hex chars).
// It uses crypto/rand so the id is unpredictable.
func newWorkerID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return "wrk_" + hex.EncodeToString(b)
}

// newSecret returns 32 random bytes encoded as hex (64 hex chars).
// It uses crypto/rand so the secret is unpredictable.
func newSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}

// handlerHeartbeat decodes and validates a heartbeat, returning 200 on success.
// Existing behavior is unchanged; authentication is added in Step 1.4.
func handlerHeartbeat(w http.ResponseWriter, r *http.Request) {
	var hb api.Heartbeat
	if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}
	if err := hb.Validate(); err != nil {
		http.Error(w, fmt.Sprintf("invalid heartbeat: %v", err), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleRegister handles POST /v1/workers/register.
// It decodes the register request, creates a worker record, stores the SHA-256
// hash of the secret, and returns the plain secret exactly once in the response.
// The secret is never logged and never stored in plaintext.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	// Limit request body to 1 MB to prevent memory exhaustion from oversized payloads.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req api.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	id := newWorkerID()
	secret := newSecret()
	secretHash := registry.HashSecret(secret)

	s.reg.Register(
		id,
		req.Specs.Hostname,
		req.Specs.OS,
		req.Specs.Arch,
		0, // cpuModel: specs.Specs.CPU is a string; Worker.CPUModel is int
		req.Specs.TotalRAM,
		secretHash,
	)

	resp := api.RegisterResponse{
		WorkerID:         id,
		Secret:           secret,
		HeartbeatSeconds: 5,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.log.Error("encode register response failed", "err", err)
	}
}
