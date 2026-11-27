package registry

import (
	"encoding/hex"
	"errors"
	"sync"
	"testing"
	"time"

	"node-agent/internal/api"
)

// fakeClock returns a clock that only advances when Advance is called.
type fakeClock struct {
	mu   sync.Mutex
	now  time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{now: start}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func mustHash(t *testing.T, secret string) [32]byte {
	t.Helper()
	return HashSecret(secret)
}

func TestRecordHeartbeatAndGet(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC))
	reg := New(clock.Now)

	hb := api.Heartbeat{
		WorkerID:          "wrk-1",
		Seq:               1,
		SentAt:            clock.Now().Add(-time.Second),
		AgentVersion:      "0.1.0",
		CPUPercent:        10,
		RAMTotalBytes:     8_000_000_000,
		RAMAvailableBytes: 4_000_000_000,
		ActiveTasks:       0,
		LoadedModels:      []string{"llama3"},
	}

	reg.RecordHeartbeat("wrk-1", hb)

	got, err := reg.Get("wrk-1")
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}

	if got.ID != "wrk-1" {
		t.Errorf("Get().ID = %q, want %q", got.ID, "wrk-1")
	}
	if got.RAMTotalBytes != 8_000_000_000 {
		t.Errorf("Get().RAMTotalBytes = %d, want 8000000000", got.RAMTotalBytes)
	}
	if !got.LastSeen.Equal(clock.Now()) {
		t.Errorf("Get().LastSeen = %v, want coordinator clock %v", got.LastSeen, clock.Now())
	}
	if got.Status != "online" {
		t.Errorf("Get().Status = %q, want %q", got.Status, "online")
	}
	// View has no SecretHash field — secrets never leave the registry.
}

func TestGetUnknownWorkerReturnsErrUnknownWorker(t *testing.T) {
	reg := New(nil)

	_, err := reg.Get("does-not-exist")
	if !errors.Is(err, ErrUnknownWorker) {
		t.Errorf("Get() error = %v, want ErrUnknownWorker", err)
	}
}

func TestCheckSecret(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC))
	reg := New(clock.Now)

	const secret = "supersafe"
	reg.RecordHeartbeat("wrk-1", api.Heartbeat{WorkerID: "wrk-1"})

	// Manually set the stored hash as registration (Step 1.3) would.
	reg.mu.Lock()
	w := reg.workers["wrk-1"]
	w.SecretHash = HashSecret(secret)
	reg.mu.Unlock()

	if !reg.CheckSecret("wrk-1", secret) {
		t.Error("CheckSecret() = false, want true for correct secret")
	}
	if reg.CheckSecret("wrk-1", "wrong-secret") {
		t.Error("CheckSecret() = true, want false for wrong secret")
	}
	if reg.CheckSecret("nope", secret) {
		t.Error("CheckSecret() = true, want false for unknown worker")
	}
	if reg.CheckSecret("wrk-1", "") {
		t.Error("CheckSecret() = true, want false for empty secret")
	}
}

func TestStatusLifecycleWithFakeClock(t *testing.T) {
	start := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)
	reg := New(clock.Now)

	reg.RecordHeartbeat("wrk-1", api.Heartbeat{WorkerID: "wrk-1"})

	v, err := reg.Get("wrk-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if v.Status != "online" {
		t.Errorf("immediately after heartbeat: status = %q, want %q", v.Status, "online")
	}

	// Advance to just under StaleAfter (15s).
	clock.Advance(14 * time.Second + 999 * time.Millisecond)
	v, _ = reg.Get("wrk-1")
	if v.Status != "online" {
		t.Errorf("at 14.999s: status = %q, want %q", v.Status, "online")
	}

	// Advance to exactly StaleAfter (15s) -> stale (strict <).
	clock.Advance(time.Millisecond)
	v, _ = reg.Get("wrk-1")
	if v.Status != "stale" {
		t.Errorf("at 15s: status = %q, want %q", v.Status, "stale")
	}

	// Advance to just under OfflineAfter (45s).
	clock.Advance(29 * time.Second + 999 * time.Millisecond)
	v, _ = reg.Get("wrk-1")
	if v.Status != "stale" {
		t.Errorf("at 44.999s: status = %q, want %q", v.Status, "stale")
	}

	// Advance to exactly OfflineAfter (45s) -> offline.
	clock.Advance(time.Millisecond)
	v, _ = reg.Get("wrk-1")
	if v.Status != "offline" {
		t.Errorf("at 45s: status = %q, want %q", v.Status, "offline")
	}
}

func TestLastSeenUsesCoordinatorClockNotAgentClock(t *testing.T) {
	// The agent's SentAt is far in the past; LastSeen must still be the
	// coordinator's receive time.
	clock := newFakeClock(time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC))
	reg := New(clock.Now)

	hb := api.Heartbeat{
		WorkerID:      "wrk-1",
		SentAt:        time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), // agent clock is wrong
		RAMTotalBytes: 8_000_000_000,
	}
	reg.RecordHeartbeat("wrk-1", hb)

	v, err := reg.Get("wrk-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !v.LastSeen.Equal(clock.Now()) {
		t.Errorf("LastSeen = %v, want coordinator clock %v (not agent SentAt %v)",
			v.LastSeen, clock.Now(), hb.SentAt)
	}
}

func TestGetReturnsDefensiveCopies(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC))
	reg := New(clock.Now)

	hb := api.Heartbeat{
		WorkerID:       "wrk-1",
		RAMTotalBytes:  8_000_000_000,
		LoadedModels:   []string{"llama3", "mistral"},
	}
	reg.RecordHeartbeat("wrk-1", hb)

	// Manually add labels as registration would.
	reg.mu.Lock()
	w := reg.workers["wrk-1"]
	w.Labels = map[string]string{"rack": "a1", "env": "prod"}
	reg.mu.Unlock()

	v, err := reg.Get("wrk-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	// Mutating the returned view must not affect storage.
	delete(v.Labels, "rack")
	v.Labels["new"] = "value"
	v.Last.LoadedModels = append(v.Last.LoadedModels, "extra-model")

	v2, err := reg.Get("wrk-1")
	if err != nil {
		t.Fatalf("second Get() error = %v", err)
	}
	if _, ok := v2.Labels["rack"]; !ok {
		t.Error("Get() returned a view whose Labels map shares state with storage")
	}
	if _, ok := v2.Labels["new"]; ok {
		t.Error("Get() leaked caller-added label into storage")
	}
	if len(v2.Last.LoadedModels) != 2 {
		t.Errorf("Get() leaked caller-appended model; got %d, want 2", len(v2.Last.LoadedModels))
	}
}

func TestListIsSortedAndReturnsCopies(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC))
	reg := New(clock.Now)

	reg.RecordHeartbeat("wrk-c", api.Heartbeat{WorkerID: "wrk-c", RAMTotalBytes: 1})
	reg.RecordHeartbeat("wrk-a", api.Heartbeat{WorkerID: "wrk-a", RAMTotalBytes: 2})
	reg.RecordHeartbeat("wrk-b", api.Heartbeat{WorkerID: "wrk-b", RAMTotalBytes: 3})

	views := reg.List()
	if len(views) != 3 {
		t.Fatalf("List() = %d workers, want 3", len(views))
	}
	wantOrder := []string{"wrk-a", "wrk-b", "wrk-c"}
	for i, id := range wantOrder {
		if views[i].ID != id {
			t.Errorf("List()[%d].ID = %q, want %q", i, views[i].ID, id)
		}
	}

	// Mutating one returned view must not affect another call to List.
	views[0].Labels = map[string]string{"mutated": "true"}
	again := reg.List()
	if _, ok := again[0].Labels["mutated"]; ok {
		t.Error("List() leaked caller mutation into subsequent List() call")
	}
}

func TestListEmpty(t *testing.T) {
	reg := New(nil)
	views := reg.List()
	if views == nil {
		t.Error("List() returned nil for empty registry; want empty slice")
	}
	if len(views) != 0 {
		t.Errorf("List() = %d workers, want 0", len(views))
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC))
	reg := New(clock.Now)

	const goroutines = 50
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 2) // writers + readers

	// Writers: record heartbeats.
	for g := 0; g < goroutines; g++ {
		go func(n int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				id := hex.EncodeToString([]byte{byte(n)}) + "-worker"
				reg.RecordHeartbeat(id, api.Heartbeat{WorkerID: id, RAMTotalBytes: uint64(i)})
				clock.Advance(time.Millisecond)
			}
		}(g)
	}

	// Readers: Get/List continuously.
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_ = reg.List()
				clock.Advance(time.Millisecond)
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All completed without data race (go test -race would catch races).
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent test did not complete in time")
	}
}
