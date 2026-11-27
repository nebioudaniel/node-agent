package registry

import (
	"crypto/sha256"
	"errors"
	"sort"
	"sync"
	"time"

	"node-agent/internal/api"
	"crypto/subtle"
)

// ErrUnknownWorker is returned when a worker id is not in the registry.
// Callers (e.g. the HTTP handler) decide what status code to map it to.
var ErrUnknownWorker = errors.New("unknown worker")

// Worker is the stored record. It includes SecretHash (never the plain
// secret) and the full last heartbeat.
type Worker struct {
	ID            string            `json:"id"`
	Hostname      string            `json:"host_name"`
	OS            string            `json:"os"`
	Arch          string            `json:"arch"`
	CPUModel      int               `json:"cpu_model"`
	RAMTotalBytes uint64            `json:"ram_total_bytes"`
	Labels        map[string]string `json:"labels"`
	SecretHash    [32]byte          `json:"secret_hash"`
	LastSeen      time.Time         `json:"last_seen"`
	Last          api.Heartbeat     `json:"last"`
	Status        string            `json:"status"`
}

// View is what leaves the registry: same fields as Worker, but SecretHash is
// omitted and Status is computed on read. Callers receive a copy, not a
// reference to live storage, so they cannot mutate internal state or read
// the secret hash.
type View struct {
	ID            string            `json:"id"`
	Hostname      string            `json:"host_name"`
	OS            string            `json:"os"`
	Arch          string            `json:"arch"`
	CPUModel      int               `json:"cpu_model"`
	RAMTotalBytes uint64            `json:"ram_total_bytes"`
	Labels        map[string]string `json:"labels"`
	LastSeen      time.Time         `json:"last_seen"`
	Last          api.Heartbeat     `json:"last"`
	Status        string            `json:"status"`
}

// Registry is an in-memory, concurrency-safe worker store. It knows nothing
// about HTTP — it only stores workers and hands out read-only views.
type Registry struct {
	mu      sync.RWMutex
	workers map[string]*Worker
	now     func() time.Time
	// reported is a placeholder for the Sweep step (Step 1.5).
	reported map[string]string
}

// New creates a Registry. now is the clock used to stamp LastSeen and to
// compute status. If now is nil, time.Now is used. In production pass nil
// (or time.Now); in tests pass a fake clock for deterministic timing.
func New(now func() time.Time) *Registry {
	if now == nil {
		now = time.Now
	}
	return &Registry{
		workers:  make(map[string]*Worker),
		now:      now,
		reported: make(map[string]string),
	}
}

// HashSecret returns the sha256 digest of secret as a fixed-size [32]byte.
// Only the hash is ever stored; the plain secret is never persisted.
func HashSecret(secret string) [32]byte {
	return sha256.Sum256([]byte(secret))
}

// RecordHeartbeat upserts a worker from a validated heartbeat. LastSeen is
// stamped with the coordinator's receive time (now()), never hb.SentAt, so
// agent clock skew cannot affect liveness. Fields not present in the
// heartbeat (Hostname, OS, Arch, CPUModel, Labels) remain zero-valued until
// the register endpoint (Step 1.3) populates them.
func (r *Registry) RecordHeartbeat(id string, hb api.Heartbeat) {
	r.mu.Lock()
	defer r.mu.Unlock()

	w := &Worker{
		ID:            id,
		RAMTotalBytes: hb.RAMTotalBytes,
		SecretHash:    [32]byte{}, // no secret yet; populated at registration
		LastSeen:      r.now(),
		Last:          hb,
		Status:        StatusFor(r.now(), r.now()),
	}
	r.workers[id] = w
}

// CheckSecret returns true if id exists and the sha256 of secret matches the
// stored SecretHash, compared in constant time to avoid timing attacks.
func (r *Registry) CheckSecret(id string, secret string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	w, ok := r.workers[id]
	if !ok {
		return false
	}
	got := HashSecret(secret)
	return subtle.ConstantTimeCompare(w.SecretHash[:], got[:]) == 1
}

// Get returns a defensive copy of the worker as a View, or ErrUnknownWorker
// if id is not present. Status is computed on read from LastSeen and now().
func (r *Registry) Get(id string) (*View, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	w, ok := r.workers[id]
	if !ok {
		return nil, ErrUnknownWorker
	}
	return r.toView(w), nil
}

// List returns a defensive copy of all workers as Views, sorted by Hostname
// then ID. Status is computed on read for each worker.
func (r *Registry) List() []*View {
	r.mu.RLock()
	defer r.mu.RUnlock()

	views := make([]*View, 0, len(r.workers))
	for _, w := range r.workers {
		views = append(views, r.toView(w))
	}
	sort.Slice(views, func(i, j int) bool {
		if views[i].Hostname != views[j].Hostname {
			return views[i].Hostname < views[j].Hostname
		}
		return views[i].ID < views[j].ID
	})
	return views
}

// Register creates a new worker record from registration data. It stores only
// the SHA-256 hash of the secret; the plain secret is returned once by the
// handler and never persisted. Status is computed on read via StatusFor.
// Caller must not hold the lock; Register acquires it internally.
func (r *Registry) Register(id, hostname, os, arch string, cpuModel int, ramTotalBytes uint64, secretHash [32]byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.workers[id] = &Worker{
		ID:            id,
		Hostname:      hostname,
		OS:            os,
		Arch:          arch,
		CPUModel:      cpuModel,
		RAMTotalBytes: ramTotalBytes,
		SecretHash:    secretHash,
		LastSeen:      r.now(),
		Status:        StatusFor(r.now(), r.now()),
	}
}

// toView builds a View from a Worker, copying maps and slices so the caller
// cannot mutate internal state. It does not include SecretHash. Status is
// computed from LastSeen using the injected clock. Caller must hold the
// registry lock (RLock or Lock) when calling this.
func (r *Registry) toView(w *Worker) *View {
	labels := make(map[string]string, len(w.Labels))
	for k, v := range w.Labels {
		labels[k] = v
	}
	loaded := make([]string, len(w.Last.LoadedModels))
	copy(loaded, w.Last.LoadedModels)

	return &View{
		ID:            w.ID,
		Hostname:      w.Hostname,
		OS:            w.OS,
		Arch:          w.Arch,
		CPUModel:      w.CPUModel,
		RAMTotalBytes: w.RAMTotalBytes,
		Labels:        labels,
		LastSeen:      w.LastSeen,
		Last:          w.Last,
		Status:        StatusFor(w.LastSeen, r.now()),
	}
}
