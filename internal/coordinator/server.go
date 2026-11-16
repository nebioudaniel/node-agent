package coordinator

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"node-agent/internal/api"
	"node-agent/internal/coordinator/registry"
)

const (
	// maxBodyBytes caps every request body so a client cannot stream an
	// unbounded amount of data at us.
	maxBodyBytes = 1 << 20 // 1 MB

	// heartbeatSeconds tells the agent how often to send a heartbeat.
	heartbeatSeconds = 5
)

// Server holds the coordinator's dependencies. Handlers are methods so they can
// reach the registry and the logger without globals.
type Server struct {
	reg *registry.Registry
	log *slog.Logger
}

// NewHandler builds the coordinator's HTTP routes.
func NewHandler(reg *registry.Registry, log *slog.Logger) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	s := &Server{reg: reg, log: log}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/workers/register", s.handleRegister)
	mux.HandleFunc("POST /v1/workers/{id}/heartbeat", s.handleHeartbeat)
	return mux
}

// handleRegister is the front door for a new worker: it mints an id and a
// secret, stores the worker with only the secret's sha256 hash, and returns the
// id and the plain secret (the only time the agent ever sees it).
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	var req api.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, fmt.Sprintf("request body too large (max %d bytes)", maxBodyBytes), http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	id, err := newWorkerID()
	if err != nil {
		s.log.Error("register: generate worker id", "err", err)
		http.Error(w, "could not generate worker id", http.StatusInternalServerError)
		return
	}
	secret, err := newSecret()
	if err != nil {
		s.log.Error("register: generate worker secret", "err", err)
		http.Error(w, "could not generate worker secret", http.StatusInternalServerError)
		return
	}

	s.reg.Add(registry.Worker{
		ID:            id,
		Hostname:      req.Specs.Hostname,
		OS:            req.Specs.OS,
		Arch:          req.Specs.Arch,
		CPUModel:      req.Specs.CPU,
		CPUCores:      req.Specs.Cores,
		RAMTotalBytes: req.Specs.TotalRAM,
		SecretHash:    registry.HashSecret(secret),
	})

	// The secret is never logged; only the id and hostname are.
	s.log.Info("worker registered", "worker_id", id, "hostname", req.Specs.Hostname)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(api.RegisterResponse{
		WorkerID:         id,
		Secret:           secret,
		HeartbeatSeconds: heartbeatSeconds,
	}); err != nil {
		s.log.Error("register: write response", "worker_id", id, "err", err)
	}
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	// decode the json body into api.Heartbeat, if the json is bad return 400
	var hb api.Heartbeat
	if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}
	// it calls hb.Validate(); if that fails it also returns 400
	if err := hb.Validate(); err != nil {
		http.Error(w, fmt.Sprintf("invalid heartbeat: %v", err), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// newWorkerID returns "wrk_" followed by 6 random bytes in hex.
func newWorkerID() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return "wrk_" + hex.EncodeToString(b[:]), nil
}

// newSecret returns 32 random bytes in hex (64 characters).
func newSecret() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
