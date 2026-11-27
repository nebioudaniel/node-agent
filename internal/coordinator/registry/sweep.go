package registry

import (
	"context"
	"log/slog"
	"sort"
	"time"
)

// Transition describes a worker status change detected by Sweep.
// From is the previously reported status; To is the newly computed status.
type Transition struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
}

// Sweep recomputes every worker's status and returns a transition for each
// worker whose current status differs from its last REPORTED status, then
// updates last-reported. Status itself is NOT set here — status is always
// computed on read (StatusFor); Sweep only tracks/reports changes.
//
// It takes the WRITE Lock (not RLock) because it mutates r.reported, even
// though it feels like a read. It does NOT log while holding the lock:
// Sweep returns data; the caller (RunSweeper) logs.
//
// Transitions are sorted by ID. A newly added worker has last-reported
// "pending". Returns a non-nil empty slice when nothing changed. Workers
// that stay in the same status across sweeps produce no transition. Both
// worsening (online->stale->offline) and recovery (offline->online) count.
func (r *Registry) Sweep() []Transition {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	out := make([]Transition, 0, len(r.workers))

	for id, w := range r.workers {
		cur := StatusFor(w.LastSeen, now)

		prev, ok := r.reported[id]
		if !ok {
			prev = "pending" // last REPORTED defaults to pending
		}

		if cur != prev {
			out = append(out, Transition{ID: id, From: prev, To: cur})
			r.reported[id] = cur // remember reported so we emit each change once
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// RunSweeper periodically runs Sweep and logs each transition. It uses
// time.NewTicker(every) + defer ticker.Stop(), selects on ctx.Done() and
// ticker.C, and returns as soon as ctx is cancelled (no goroutine leak).
// It does NOT set worker status. If log is nil, slog.Default() is used.
func RunSweeper(ctx context.Context, r *Registry, every time.Duration, log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	if every <= 0 {
		every = time.Second
	}

	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, tr := range r.Sweep() {
				log.Info("worker status transition",
					"id", tr.ID,
					"from", tr.From,
					"to", tr.To,
				)
			}
		}
	}
}
