package registry

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"node-agent/internal/api"
)

func hb(id string) api.Heartbeat {
	return api.Heartbeat{WorkerID: id, RAMTotalBytes: 8_000_000_000}
}

// TestSweepOnlineStaleOfflineTwoTransitions: online->stale->offline produces
// exactly 2 transitions total across many sweeps; repeated sweeps with no time
// change produce zero.
func TestSweepOnlineStaleOfflineTwoTransitions(t *testing.T) {
	start := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)
	reg := New(clock.Now)

	reg.Register("wrk-1", "host", "darwin", "amd64", "", 0, 8_000_000_000, [32]byte{})
	reg.RecordHeartbeat("wrk-1", hb("wrk-1")) // LastSeen = start

	// Baseline sweep at t0: pending -> online (clear it so we can count the rest).
	if got := reg.Sweep(); len(got) != 1 || got[0].From != "pending" || got[0].To != "online" {
		t.Fatalf("baseline sweep = %+v, want [pending->online]", got)
	}

	// Repeated sweeps with NO time change: zero transitions.
	for i := 0; i < 3; i++ {
		if got := reg.Sweep(); len(got) != 0 {
			t.Fatalf("sweep with no time change returned %+v, want empty", got)
		}
	}

	total := 0

	// Advance 20s -> stale (online->stale).
	clock.Advance(20 * time.Second)
	if got := reg.Sweep(); len(got) != 1 || got[0].From != "online" || got[0].To != "stale" {
		t.Fatalf("at 20s sweep = %+v, want [online->stale]", got)
	} else {
		total++
	}
	// Repeated sweep, no time change: zero.
	if got := reg.Sweep(); len(got) != 0 {
		t.Fatalf("repeated stale sweep = %+v, want empty", got)
	}

	// Advance to 50s total -> offline (stale->offline).
	clock.Advance(30 * time.Second)
	if got := reg.Sweep(); len(got) != 1 || got[0].From != "stale" || got[0].To != "offline" {
		t.Fatalf("at 50s sweep = %+v, want [stale->offline]", got)
	} else {
		total++
	}
	// Repeated sweep, no time change: zero.
	if got := reg.Sweep(); len(got) != 0 {
		t.Fatalf("repeated offline sweep = %+v, want empty", got)
	}

	if total != 2 {
		t.Fatalf("got %d transitions, want exactly 2 (online->stale, stale->offline)", total)
	}
}

// TestSweepPendingToOnlineOnFirstHeartbeat.
func TestSweepPendingToOnlineOnFirstHeartbeat(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC))
	reg := New(clock.Now)

	reg.Register("wrk-1", "host", "darwin", "amd64", "", 0, 8_000_000_000, [32]byte{})

	// First heartbeat -> online. Transition from pending.
	reg.RecordHeartbeat("wrk-1", hb("wrk-1"))

	got := reg.Sweep()
	if len(got) != 1 || got[0].From != "pending" || got[0].To != "online" {
		t.Fatalf("sweep after first heartbeat = %+v, want [pending->online]", got)
	}
}

// TestSweepOfflineToOnlineAfterNewHeartbeat.
func TestSweepOfflineToOnlineAfterNewHeartbeat(t *testing.T) {
	start := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)
	reg := New(clock.Now)

	reg.Register("wrk-1", "host", "darwin", "amd64", "", 0, 8_000_000_000, [32]byte{})
	reg.RecordHeartbeat("wrk-1", hb("wrk-1"))

	// Establish online baseline, then drive offline.
	reg.Sweep()                       // pending->online
	clock.Advance(50 * time.Second)   // -> offline
	if got := reg.Sweep(); len(got) != 1 || got[0].To != "offline" {
		t.Fatalf("driving offline: sweep = %+v, want ->offline", got)
	}

	// New heartbeat -> online. Transition offline->online.
	reg.RecordHeartbeat("wrk-1", hb("wrk-1")) // LastSeen = current (start+50s)
	got := reg.Sweep()
	if len(got) != 1 || got[0].From != "offline" || got[0].To != "online" {
		t.Fatalf("after new heartbeat sweep = %+v, want [offline->online]", got)
	}
}

// TestSweepTwoWorkersSortedByID.
func TestSweepTwoWorkersSortedByID(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC))
	reg := New(clock.Now)

	reg.Register("wrk-b", "host-b", "darwin", "amd64", "", 0, 8_000_000_000, [32]byte{})
	reg.Register("wrk-a", "host-a", "darwin", "amd64", "", 0, 8_000_000_000, [32]byte{})
	reg.RecordHeartbeat("wrk-b", hb("wrk-b"))
	reg.RecordHeartbeat("wrk-a", hb("wrk-a"))

	got := reg.Sweep()
	if len(got) != 2 {
		t.Fatalf("got %d transitions, want 2: %+v", len(got), got)
	}
	// Both pending->online, sorted by ID.
	if got[0].ID != "wrk-a" || got[1].ID != "wrk-b" {
		t.Errorf("not sorted by ID: %+v", got)
	}
	if got[0].From != "pending" || got[0].To != "online" || got[1].From != "pending" || got[1].To != "online" {
		t.Errorf("unexpected transitions: %+v", got)
	}
}

// TestSweepEmptyRegistry.
func TestSweepEmptyRegistry(t *testing.T) {
	reg := New(nil)
	got := reg.Sweep()
	if got == nil {
		t.Fatal("Sweep() returned nil, want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("Sweep() = %+v, want empty", got)
	}
}

// syncBuffer is a concurrency-safe bytes.Buffer for slog output in tests.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestSweepRunSweeper: short interval logs a transition, then cancel returns fast.
func TestSweepRunSweeper(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC))
	reg := New(clock.Now)

	reg.Register("wrk-1", "host", "darwin", "amd64", "", 0, 8_000_000_000, [32]byte{})
	reg.RecordHeartbeat("wrk-1", hb("wrk-1"))

	logBuf := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, nil))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		RunSweeper(ctx, reg, 10*time.Millisecond, logger)
		close(done)
	}()

	// Wait for a transition line to appear (bounded).
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if strings.Contains(logBuf.String(), "worker status transition") {
			break
		}
		time.Sleep(time.Millisecond)
	}

	cancel()
	select {
	case <-done:
		// goroutine returned
	case <-time.After(100 * time.Millisecond):
		t.Fatal("RunSweeper did not return within 100ms after cancel")
	}

	out := logBuf.String()
	if !strings.Contains(out, "worker status transition") {
		t.Fatalf("no transition line logged; buffer=%q", out)
	}
	if !strings.Contains(out, "wrk-1") {
		t.Fatalf("transition line missing id; buffer=%q", out)
	}
}
