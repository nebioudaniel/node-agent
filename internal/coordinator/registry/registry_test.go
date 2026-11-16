package registry

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"node-agent/internal/api"
)

// fakeClock is a clock the test controls. It has a mutex because the
// concurrency test reads it from many goroutines at once.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func testWorker(id, hostname, secret string) Worker {
	return Worker{
		ID:            id,
		Hostname:      hostname,
		OS:            "linux",
		Arch:          "amd64",
		CPUModel:      "test cpu",
		CPUCores:      4,
		RAMTotalBytes: 8_000_000_000,
		Labels:        map[string]string{"lab": "a"},
		SecretHash:    HashSecret(secret),
	}
}

func testHeartbeat(id string) api.Heartbeat {
	return api.Heartbeat{
		WorkerID:          id,
		Seq:               1,
		AgentVersion:      "0.1.0",
		RAMTotalBytes:     8_000_000_000,
		RAMAvailableBytes: 4_000_000_000,
	}
}

func TestWorkerLifecycleWithFakeClock(t *testing.T) {
	clk := newFakeClock()
	r := New(clk.Now)
	r.Add(testWorker("wrk_1", "lab-a", "s3cret"))

	v, ok := r.Get("wrk_1")
	if !ok {
		t.Fatal("worker not found after Add")
	}
	if v.Status != "pending" {
		t.Errorf("before first heartbeat: status = %q, want pending", v.Status)
	}

	if err := r.RecordHeartbeat("wrk_1", testHeartbeat("wrk_1")); err != nil {
		t.Fatalf("RecordHeartbeat: %v", err)
	}
	v, _ = r.Get("wrk_1")
	if v.Status != "online" {
		t.Errorf("right after heartbeat: status = %q, want online", v.Status)
	}

	clk.Advance(20 * time.Second)
	v, _ = r.Get("wrk_1")
	if v.Status != "stale" {
		t.Errorf("after 20s: status = %q, want stale", v.Status)
	}

	clk.Advance(30 * time.Second) // 50s since the heartbeat
	v, _ = r.Get("wrk_1")
	if v.Status != "offline" {
		t.Errorf("after 50s: status = %q, want offline", v.Status)
	}

	if err := r.RecordHeartbeat("wrk_1", testHeartbeat("wrk_1")); err != nil {
		t.Fatalf("RecordHeartbeat: %v", err)
	}
	v, _ = r.Get("wrk_1")
	if v.Status != "online" {
		t.Errorf("after a new heartbeat: status = %q, want online", v.Status)
	}
}

func TestLastSeenUsesCoordinatorClock(t *testing.T) {
	clk := newFakeClock()
	r := New(clk.Now)
	r.Add(testWorker("wrk_1", "lab-a", "s"))

	hb := testHeartbeat("wrk_1")
	hb.SentAt = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC) // the agent's clock is wildly wrong
	if err := r.RecordHeartbeat("wrk_1", hb); err != nil {
		t.Fatalf("RecordHeartbeat: %v", err)
	}

	v, _ := r.Get("wrk_1")
	if !v.LastSeen.Equal(clk.Now()) {
		t.Errorf("LastSeen = %v, want the coordinator time %v", v.LastSeen, clk.Now())
	}
	if v.Status != "online" {
		t.Errorf("status = %q, want online (SentAt must be ignored)", v.Status)
	}
}

func TestRecordHeartbeatUnknownWorker(t *testing.T) {
	r := New(newFakeClock().Now)

	err := r.RecordHeartbeat("wrk_nope", testHeartbeat("wrk_nope"))
	if !errors.Is(err, ErrUnknownWorker) {
		t.Fatalf("err = %v, want ErrUnknownWorker", err)
	}
	if _, ok := r.Get("wrk_nope"); ok {
		t.Error("Get returned ok for an unknown worker")
	}
}

func TestCheckSecret(t *testing.T) {
	r := New(newFakeClock().Now)
	r.Add(testWorker("wrk_1", "lab-a", "right-secret"))

	tests := []struct {
		name   string
		id     string
		secret string
		want   bool
	}{
		{"right secret", "wrk_1", "right-secret", true},
		{"wrong secret", "wrk_1", "wrong-secret", false},
		{"empty secret", "wrk_1", "", false},
		{"unknown worker", "wrk_nope", "right-secret", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := r.CheckSecret(tc.id, tc.secret); got != tc.want {
				t.Errorf("CheckSecret(%q, %q) = %v, want %v", tc.id, tc.secret, got, tc.want)
			}
		})
	}
}

func TestListSortedByHostname(t *testing.T) {
	r := New(newFakeClock().Now)
	r.Add(testWorker("wrk_c", "charlie", "s"))
	r.Add(testWorker("wrk_a", "alpha", "s"))
	r.Add(testWorker("wrk_b", "bravo", "s"))

	got := r.List()
	want := []string{"alpha", "bravo", "charlie"}
	if len(got) != len(want) {
		t.Fatalf("List returned %d workers, want %d", len(got), len(want))
	}
	for i, v := range got {
		if v.Hostname != want[i] {
			t.Errorf("List()[%d].Hostname = %q, want %q", i, v.Hostname, want[i])
		}
	}
}

func TestAddCopiesLabels(t *testing.T) {
	r := New(newFakeClock().Now)
	w := testWorker("wrk_1", "lab-a", "s")
	r.Add(w)

	w.Labels["lab"] = "changed after Add"

	v, _ := r.Get("wrk_1")
	if v.Labels["lab"] != "a" {
		t.Errorf("registry label = %q, want it unaffected by the caller's later change", v.Labels["lab"])
	}
}

func TestViewsAreCopies(t *testing.T) {
	r := New(newFakeClock().Now)
	r.Add(testWorker("wrk_1", "lab-a", "s"))

	hb := testHeartbeat("wrk_1")
	hb.LoadedModels = []string{"llama3"}
	if err := r.RecordHeartbeat("wrk_1", hb); err != nil {
		t.Fatalf("RecordHeartbeat: %v", err)
	}

	// Mangle everything we were handed back.
	v, _ := r.Get("wrk_1")
	v.Labels["lab"] = "changed"
	v.Labels["extra"] = "x"
	v.Last.LoadedModels[0] = "changed"

	list := r.List()
	list[0].Labels["lab"] = "changed again"

	// The registry must be untouched.
	again, _ := r.Get("wrk_1")
	if again.Labels["lab"] != "a" || len(again.Labels) != 1 {
		t.Errorf("labels were modified through a View: %v", again.Labels)
	}
	if again.Last.LoadedModels[0] != "llama3" {
		t.Errorf("LoadedModels was modified through a View: %v", again.Last.LoadedModels)
	}
}

func TestConcurrentHeartbeatAndList(t *testing.T) {
	clk := newFakeClock()
	r := New(clk.Now)
	for i := 0; i < 5; i++ {
		r.Add(testWorker(fmt.Sprintf("wrk_%d", i), fmt.Sprintf("host-%d", i), "s"))
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("wrk_%d", i%5)
			for j := 0; j < 100; j++ {
				if err := r.RecordHeartbeat(id, testHeartbeat(id)); err != nil {
					t.Errorf("RecordHeartbeat(%s): %v", id, err)
					return
				}
				_ = r.List()
				_, _ = r.Get(id)
				_ = r.CheckSecret(id, "s")
				clk.Advance(time.Millisecond)
			}
		}(i)
	}
	wg.Wait()

	if got := len(r.List()); got != 5 {
		t.Errorf("List returned %d workers, want 5", got)
	}
}
