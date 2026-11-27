package coordinator

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"node-agent/internal/api"
	"node-agent/internal/coordinator/registry"
)

// Server holds the dependencies for coordinator HTTP handlers.
type Server struct {
	reg *registry.Registry
	log *slog.Logger
}

// NewHandler builds the coordinator HTTP mux with the heartbeat, register, and list routes.
// The registry is injected so it can be shared and tested independently.
func NewHandler(reg *registry.Registry) http.Handler {
	s := &Server{
		reg: reg,
		log: slog.Default(),
	}
	mux := http.NewServeMux()
	// POST /v1/workers/{id}/heartbeat is wrapped by requireWorker authentication.
	mux.HandleFunc("POST /v1/workers/{id}/heartbeat", s.requireWorker(s.handleHeartbeat))
	mux.HandleFunc("POST /v1/workers/register", s.handleRegister)
	mux.HandleFunc("GET /v1/workers", s.handleListWorkers)
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

// requireWorker is HTTP middleware that authenticates a worker.
// It checks (in order): (1) worker id in path exists in registry -> 404,
// (2) Authorization header has "Bearer <secret>" with correct secret -> 401.
// Checks 404 BEFORE 401 so we don't reveal whether a worker id exists.
// On success, it calls next. Secrets are never logged.
func (s *Server) requireWorker(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		// Check 404 first: does this worker exist?
		if _, err := s.reg.Get(id); err != nil {
			if errors.Is(err, registry.ErrUnknownWorker) {
				http.Error(w, "unknown worker", http.StatusNotFound)
				return
			}
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// Check 401 second: is the secret correct?
		secret, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok || secret == "" || !s.reg.CheckSecret(id, secret) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" header.
// Returns ok=false if the header is missing, wrong scheme, or empty token.
func bearerToken(header string) (token string, ok bool) {
	if header == "" {
		return "", false
	}
	// Scheme is case-insensitive per HTTP spec.
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(parts[1])
	if token == "" {
		return "", false
	}
	return token, true
}

// handleHeartbeat decodes and validates a heartbeat, records it in the registry,
// and returns 200 with api.HeartbeatResponse. Authentication is handled by
// requireWorker. Body is limited to 1 MB. Never trusts hb.SentAt for LastSeen.
func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Limit request body to 1 MB to prevent memory exhaustion.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var hb api.Heartbeat
	if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Body limit validation.
	if err := hb.Validate(); err != nil {
		http.Error(w, fmt.Sprintf("invalid heartbeat: %v", err), http.StatusBadRequest)
		return
	}

	// WorkerID in body must match the id in the path.
	if hb.WorkerID != id {
		http.Error(w, fmt.Sprintf("worker_id mismatch: body %q, path %q", hb.WorkerID, id), http.StatusBadRequest)
		return
	}

	// RecordHeartbeat stamps LastSeen with the coordinator's clock, never hb.SentAt.
	s.reg.RecordHeartbeat(id, hb)

	resp := api.HeartbeatResponse{
		NextHeartbeatSeconds: 5,
		MinAgentVersion:      "0.1.0",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.log.Error("encode heartbeat response failed", "err", err)
	}
}

// handleListWorkers returns registry.List() as JSON. Returns [] (not null) when empty.
func (s *Server) handleListWorkers(w http.ResponseWriter, r *http.Request) {
	workers := s.reg.List() // List returns non-nil empty slice when no workers.

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(workers); err != nil {
		s.log.Error("encode worker list failed", "err", err)
	}
}

// handleRegister handles POST /v1/workers/register.
// It decodes the register request, creates a worker record, stores the SHA-256
// hash of the secret, and returns the plain secret exactly once in the response.
// The secret is never logged and never stored in plaintext.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	// Limit request body to 1 MB to prevent memory exhaustion from oversized payloads.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	var req api.RegisterRequest
	if err := json.Unmarshal(body, &req); err != nil {
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
		req.Specs.CPU,      // cpuModel
		req.Specs.Cores,    // cpuCores
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
