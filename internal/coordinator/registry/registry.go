// Package registry keeps the coordinator's in-memory list of workers.
package registry

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"sort"
	"sync"
	"time"

	"node-agent/internal/api"
)

// ErrUnknownWorker is returned when a worker id is not registered.
var ErrUnknownWorker = errors.New("unknown worker")

// Worker is the stored record for one worker. It contains the secret hash, so
// it is only ever passed IN to the registry (Add). Anything handed back out is
// a View, which has no secret.
type Worker struct {
	ID            string
	Hostname      string
	OS            string
	Arch          string
	CPUModel      string
	CPUCores      int
	RAMTotalBytes uint64
	Labels        map[string]string
	SecretHash    [32]byte
	LastSeen      time.Time
	Last          api.Heartbeat
}

// View is a read-only copy of a Worker for the outside world: the same fields,
// no SecretHash, plus the computed Status. The json tags make it the shape of
// GET /v1/workers.
type View struct {
	ID            string            `json:"id"`
	Hostname      string            `json:"hostname"`
	OS            string            `json:"os"`
	Arch          string            `json:"arch"`
	CPUModel      string            `json:"cpu_model"`
	CPUCores      int               `json:"cpu_cores"`
	RAMTotalBytes uint64            `json:"ram_total_bytes"`
	Labels        map[string]string `json:"labels"`
	LastSeen      time.Time         `json:"last_seen"`
	Last          api.Heartbeat     `json:"last_heartbeat"`
	Status        string            `json:"status"`
}

// Registry stores workers and answers "who is alive". It never calls
// time.Now() itself: the clock is injected so tests can control time.
type Registry struct {
	mu       sync.RWMutex
	workers  map[string]*Worker
	reported map[string]string // last status announced by Sweep (used in step 1.5)
	now      func() time.Time
}

// New creates an empty registry. Pass time.Now in production and a fake clock
// in tests.
func New(now func() time.Time) *Registry {
	if now == nil {
		now = time.Now
	}
	return &Registry{
		workers:  make(map[string]*Worker),
		reported: make(map[string]string),
		now:      now,
	}
}

// HashSecret returns the sha256 of a worker secret. Only the hash is stored.
func HashSecret(secret string) [32]byte {
	return sha256.Sum256([]byte(secret))
}

// Add stores a worker (replacing any worker with the same id). The maps and
// slices inside are copied so the caller cannot change them afterwards.
func (r *Registry) Add(w Worker) {
	w.Labels = copyLabels(w.Labels)
	w.Last.LoadedModels = copyStrings(w.Last.LoadedModels)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.workers[w.ID] = &w
}

// CheckSecret reports whether secret is the right secret for worker id.
// It compares sha256 hashes in constant time.
func (r *Registry) CheckSecret(id, secret string) bool {
	r.mu.RLock()
	w, ok := r.workers[id]
	var stored [32]byte
	if ok {
		stored = w.SecretHash
	}
	r.mu.RUnlock()

	if !ok {
		return false
	}
	sum := sha256.Sum256([]byte(secret))
	return subtle.ConstantTimeCompare(sum[:], stored[:]) == 1
}

// RecordHeartbeat stores the heartbeat and stamps LastSeen with the
// COORDINATOR's clock. hb.SentAt (the agent's clock) is never trusted.
func (r *Registry) RecordHeartbeat(id string, hb api.Heartbeat) error {
	now := r.now()
	hb.LoadedModels = copyStrings(hb.LoadedModels)

	r.mu.Lock()
	defer r.mu.Unlock()

	w, ok := r.workers[id]
	if !ok {
		return ErrUnknownWorker
	}
	w.LastSeen = now
	w.Last = hb
	return nil
}

// List returns a copy of every worker, sorted by Hostname (then id).
func (r *Registry) List() []View {
	now := r.now()

	r.mu.RLock()
	views := make([]View, 0, len(r.workers))
	for _, w := range r.workers {
		views = append(views, toView(w, now))
	}
	r.mu.RUnlock()

	sort.Slice(views, func(i, j int) bool {
		if views[i].Hostname != views[j].Hostname {
			return views[i].Hostname < views[j].Hostname
		}
		return views[i].ID < views[j].ID
	})
	return views
}

// Get returns a copy of one worker.
func (r *Registry) Get(id string) (View, bool) {
	now := r.now()

	r.mu.RLock()
	defer r.mu.RUnlock()

	w, ok := r.workers[id]
	if !ok {
		return View{}, false
	}
	return toView(w, now), true
}

// toView copies a Worker into a View. The caller must hold at least RLock.
func toView(w *Worker, now time.Time) View {
	last := w.Last
	last.LoadedModels = copyStrings(w.Last.LoadedModels)

	return View{
		ID:            w.ID,
		Hostname:      w.Hostname,
		OS:            w.OS,
		Arch:          w.Arch,
		CPUModel:      w.CPUModel,
		CPUCores:      w.CPUCores,
		RAMTotalBytes: w.RAMTotalBytes,
		Labels:        copyLabels(w.Labels),
		LastSeen:      w.LastSeen,
		Last:          last,
		Status:        StatusFor(w.LastSeen, now),
	}
}

func copyLabels(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyStrings(in []string) []string {
	return append([]string(nil), in...)
}
