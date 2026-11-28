package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"node-agent/internal/api"
	"node-agent/internal/specs"
)

// fakeCoord is a minimal coordinator for client tests.
type fakeCoord struct {
	mu sync.Mutex

	regCount int32
	hbCount  int32

	hbStatusFn func(n int) int
	hbDelay    time.Duration

	seqs  []int
	auths []string

	workerID string
	secret   string
}

func newFakeCoord(hbStatusFn func(n int) int) *fakeCoord {
	return &fakeCoord{
		hbStatusFn: hbStatusFn,
		workerID:   "wrk-fake-1",
		secret:     "secret-fake-1",
	}
}

func (fc *fakeCoord) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/workers/register", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fc.regCount, 1)
		resp := api.RegisterResponse{
			WorkerID:         fc.workerID,
			Secret:           fc.secret,
			HeartbeatSeconds: 5,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("POST /v1/workers/{id}/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		n := int(atomic.AddInt32(&fc.hbCount, 1))
		auth := r.Header.Get("Authorization")
		var hb api.Heartbeat
		json.NewDecoder(r.Body).Decode(&hb)

		fc.mu.Lock()
		fc.seqs = append(fc.seqs, int(hb.Seq))
		fc.auths = append(fc.auths, auth)
		fc.mu.Unlock()

		status := http.StatusOK
		if fc.hbStatusFn != nil {
			status = fc.hbStatusFn(n)
		}
		if fc.hbDelay > 0 {
			time.Sleep(fc.hbDelay)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status == http.StatusOK {
			json.NewEncoder(w).Encode(api.HeartbeatResponse{
				NextHeartbeatSeconds: 5,
				MinAgentVersion:      "0.1.0",
			})
		}
	})
	return mux
}

func (fc *fakeCoord) regCountVal() int { return int(atomic.LoadInt32(&fc.regCount)) }
func (fc *fakeCoord) hbCountVal() int  { return int(atomic.LoadInt32(&fc.hbCount)) }

func (fc *fakeCoord) seqsCopy() []int {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	out := make([]int, len(fc.seqs))
	copy(out, fc.seqs)
	return out
}

func (fc *fakeCoord) authsCopy() []string {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	out := make([]string, len(fc.auths))
	copy(out, fc.auths)
	return out
}

func testSpecs() specs.Specs {
	return specs.Specs{Hostname: "test", OS: "darwin", Arch: "amd64", CPU: "Test CPU", Cores: 4, TotalRAM: 8_000_000_000}
}

type testHandler struct{}

func (h *testHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *testHandler) Handle(context.Context, slog.Record) error { return nil }
func (h *testHandler) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *testHandler) WithGroup(string) slog.Handler             { return h }

func newTestLogger() *slog.Logger {
	return slog.New(&testHandler{})
}

func TestClientRunNormalSeq(t *testing.T) {
	fc := newFakeCoord(func(n int) int { return http.StatusOK })
	srv := httptest.NewServer(fc.handler())
	defer srv.Close()

	dir := t.TempDir()
	c := NewClient(srv.URL, dir, newTestLogger())
	c.heartbeatInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx, "tok", testSpecs()); close(done) }()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if fc.hbCountVal() >= 3 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Run did not return within 200ms after cancel")
	}

	seqs := fc.seqsCopy()
	if len(seqs) < 3 {
		t.Fatalf("got %d heartbeats, want >=3", len(seqs))
	}
	for i := 0; i < 3; i++ {
		if seqs[i] != i+1 {
			t.Errorf("seq[%d] = %d, want %d", i, seqs[i], i+1)
		}
	}
	auths := fc.authsCopy()
	for _, a := range auths {
		if a != "Bearer "+fc.secret {
			t.Errorf("Authorization = %q, want %q", a, "Bearer "+fc.secret)
		}
	}
}

func TestClientRun404Reregisters(t *testing.T) {
	var hbN int32
	fc := newFakeCoord(func(n int) int {
		if atomic.CompareAndSwapInt32(&hbN, 0, 1) {
			return http.StatusNotFound
		}
		return http.StatusOK
	})
	srv := httptest.NewServer(fc.handler())
	defer srv.Close()

	dir := t.TempDir()
	c := NewClient(srv.URL, dir, newTestLogger())
	c.heartbeatInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx, "tok", testSpecs()); close(done) }()

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if fc.regCountVal() >= 2 && fc.hbCountVal() >= 3 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done

	if fc.regCountVal() < 2 {
		t.Errorf("regCount = %d, want >=2 (re-register after 404)", fc.regCountVal())
	}
}

func TestClientRun500Then200(t *testing.T) {
	var hbN int32
	fc := newFakeCoord(func(n int) int {
		v := atomic.AddInt32(&hbN, 1)
		if v <= 3 {
			return http.StatusInternalServerError
		}
		return http.StatusOK
	})
	srv := httptest.NewServer(fc.handler())
	defer srv.Close()

	dir := t.TempDir()
	c := NewClient(srv.URL, dir, newTestLogger())
	c.heartbeatInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx, "tok", testSpecs()); close(done) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fc.hbCountVal() >= 5 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done

	if fc.hbCountVal() < 4 {
		t.Errorf("hbCount = %d, want >=4 (recovery after 500s)", fc.hbCountVal())
	}
}

func TestClientRunCancelWithin100ms(t *testing.T) {
	fc := newFakeCoord(func(n int) int { return http.StatusOK })
	srv := httptest.NewServer(fc.handler())
	defer srv.Close()

	dir := t.TempDir()
	c := NewClient(srv.URL, dir, newTestLogger())
	c.heartbeatInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx, "tok", testSpecs()); close(done) }()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Run did not return within 100ms after cancel")
	}
}

func TestClientNoIdentityRegistersAndSaves(t *testing.T) {
	fc := newFakeCoord(func(n int) int { return http.StatusOK })
	srv := httptest.NewServer(fc.handler())
	defer srv.Close()

	dir := t.TempDir()
	c := NewClient(srv.URL, dir, newTestLogger())
	c.heartbeatInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx, "tok", testSpecs()); close(done) }()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if fc.hbCountVal() >= 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done

	id, err := LoadIdentity(dir)
	if err != nil {
		t.Fatalf("LoadIdentity: %v", err)
	}
	if id == nil || id.WorkerID != fc.workerID || id.Secret != fc.secret {
		t.Errorf("identity = %+v, want %s/%s", id, fc.workerID, fc.secret)
	}
}

func TestClientCoordinatorDownAtStart(t *testing.T) {
	dir := t.TempDir()
	c := NewClient("http://127.0.0.1:1", dir, newTestLogger())
	c.heartbeatInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Run panicked: %v", r)
			}
		}()
		c.Run(ctx, "tok", testSpecs())
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Run did not return within 100ms after cancel")
	}
}

func TestClient400NoTightLoop(t *testing.T) {
	fc := newFakeCoord(func(n int) int { return http.StatusBadRequest })
	srv := httptest.NewServer(fc.handler())
	defer srv.Close()

	dir := t.TempDir()
	c := NewClient(srv.URL, dir, newTestLogger())
	c.heartbeatInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx, "tok", testSpecs()); close(done) }()

	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	if fc.hbCountVal() > 20 {
		t.Errorf("hbCount = %d, possible tight loop on 400", fc.hbCountVal())
	}
}
