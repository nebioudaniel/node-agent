package coordinator

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"node-agent/internal/api"
	"node-agent/internal/coordinator/registry"
	"node-agent/internal/specs"
)

const validBody = `{"worker_id":"wrk_test","seq":1,"agent_version":"0.1.0","cpu_percent":10,"ram_total_bytes":8000000000,"ram_available_bytes":4000000000,"active_tasks":0,"loaded_models":[]}`

func TestHeartbeatHandler(t *testing.T) {
	reg := registry.New(nil)
	s := &Server{reg: reg, log: slog.Default()}
	// Test handleHeartbeat body validation through a plain mux (no auth
	// middleware) so we isolate the decode/validate logic.
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/workers/{id}/heartbeat", s.handleHeartbeat)

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantInBody string
	}{
		{"valid", validBody, http.StatusOK, ""},
		{"ram available > total",
			strings.Replace(validBody, `"ram_available_bytes":4000000000`, `"ram_available_bytes":9000000000`, 1),
			http.StatusBadRequest, "ram_available_bytes"},
		{"empty worker id",
			strings.Replace(validBody, `"worker_id":"wrk_test"`, `"worker_id":""`, 1),
			http.StatusBadRequest, "worker_id"},
		{"invalid json", `{nope`, http.StatusBadRequest, "invalid JSON"},
		{"worker_id mismatch",
			strings.Replace(validBody, `"worker_id":"wrk_test"`, `"worker_id":"wrk_other"`, 1),
			http.StatusBadRequest, "worker_id"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/workers/wrk_test/heartbeat", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantInBody != "" && !strings.Contains(rec.Body.String(), tc.wantInBody) {
				t.Errorf("body %q does not contain %q", rec.Body.String(), tc.wantInBody)
			}
		})
	}
}

func TestNewWorkerID(t *testing.T) {
	id1 := newWorkerID()
	id2 := newWorkerID()

	if !strings.HasPrefix(id1, "wrk_") {
		t.Errorf("newWorkerID() = %q, want prefix %q", id1, "wrk_")
	}
	if !strings.HasPrefix(id2, "wrk_") {
		t.Errorf("newWorkerID() = %q, want prefix %q", id2, "wrk_")
	}
	// "wrk_" + 6 bytes hex = 4 + 12 = 16 chars
	if len(id1) != 16 {
		t.Errorf("len(newWorkerID()) = %d, want 16", len(id1))
	}
	if id1 == id2 {
		t.Error("two newWorkerID() calls returned the same id")
	}
	// Verify the suffix is valid hex.
	for _, id := range []string{id1, id2} {
		if _, err := hex.DecodeString(id[4:]); err != nil {
			t.Errorf("newWorkerID() suffix %q is not valid hex: %v", id[4:], err)
		}
	}
}

func TestNewSecret(t *testing.T) {
	s1 := newSecret()
	s2 := newSecret()

	// 32 bytes hex = 64 chars
	if len(s1) != 64 {
		t.Errorf("len(newSecret()) = %d, want 64", len(s1))
	}
	if len(s2) != 64 {
		t.Errorf("len(newSecret()) = %d, want 64", len(s2))
	}
	if s1 == s2 {
		t.Error("two newSecret() calls returned the same secret")
	}
	// Verify valid hex.
	for _, s := range []string{s1, s2} {
		if _, err := hex.DecodeString(s); err != nil {
			t.Errorf("newSecret() value %.8q... is not valid hex: %v", s[:8], err)
		}
	}
}

func TestHandleRegister(t *testing.T) {
	reg := registry.New(nil)
	s := &Server{reg: reg, log: slog.Default()}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/workers/register", s.handleRegister)

	specs := specs.Specs{
		Hostname: "test-machine",
		OS:       "darwin",
		Arch:     "amd64",
		CPU:      "Test CPU",
		Cores:    4,
		TotalRAM: 8_000_000_000,
	}
	specsJSON, _ := json.Marshal(specs)

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantInBody string
		method     string
	}{
		{"valid",
			`{"join_token":"dev","specs":` + string(specsJSON) + `}`,
			http.StatusCreated,
			"wrk_",
			http.MethodPost,
		},
		{"invalid json", `not json`, http.StatusBadRequest, "invalid JSON", http.MethodPost},
		{"empty body", ``, http.StatusBadRequest, "invalid JSON", http.MethodPost},
		{"wrong method", "", http.StatusMethodNotAllowed, "", http.MethodGet},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var body io.Reader
			if tc.body != "" {
				body = strings.NewReader(tc.body)
			}
			req := httptest.NewRequest(tc.method, "/v1/workers/register", body)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantInBody != "" && !strings.Contains(rec.Body.String(), tc.wantInBody) {
				t.Errorf("body %q does not contain %q", rec.Body.String(), tc.wantInBody)
			}
		})
	}
}

func TestHandleRegisterCreatesWorkerInRegistry(t *testing.T) {
	reg := registry.New(nil)
	s := &Server{reg: reg, log: slog.Default()}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/workers/register", s.handleRegister)

	specs := specs.Specs{
		Hostname: "test-machine",
		OS:       "darwin",
		Arch:     "amd64",
		CPU:      "Test CPU",
		Cores:    4,
		TotalRAM: 8_000_000_000,
	}
	specsJSON, _ := json.Marshal(specs)

	req := httptest.NewRequest(http.MethodPost, "/v1/workers/register",
		strings.NewReader(`{"join_token":"dev","specs":`+string(specsJSON)+`}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	var resp api.RegisterResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.WorkerID == "" {
		t.Error("RegisterResponse.WorkerID is empty")
	}
	if !strings.HasPrefix(resp.WorkerID, "wrk_") {
		t.Errorf("RegisterResponse.WorkerID = %q, want prefix %q", resp.WorkerID, "wrk_")
	}
	if len(resp.Secret) != 64 {
		t.Errorf("len(RegisterResponse.Secret) = %d, want 64", len(resp.Secret))
	}
	if resp.HeartbeatSeconds != 5 {
		t.Errorf("RegisterResponse.HeartbeatSeconds = %d, want 5", resp.HeartbeatSeconds)
	}

	// The worker should be findable in the registry and the secret should verify.
	v, err := reg.Get(resp.WorkerID)
	if err != nil {
		t.Fatalf("reg.Get(%q) error = %v, want nil", resp.WorkerID, err)
	}
	if v.Hostname != "test-machine" {
		t.Errorf("view.Hostname = %q, want %q", v.Hostname, "test-machine")
	}
	if v.OS != "darwin" {
		t.Errorf("view.OS = %q, want %q", v.OS, "darwin")
	}
	if v.Arch != "amd64" {
		t.Errorf("view.Arch = %q, want %q", v.Arch, "amd64")
	}
	if v.RAMTotalBytes != 8_000_000_000 {
		t.Errorf("view.RAMTotalBytes = %d, want 8000000000", v.RAMTotalBytes)
	}
	if !reg.CheckSecret(resp.WorkerID, resp.Secret) {
		t.Error("CheckSecret(id, secret) = false, want true")
	}
	if reg.CheckSecret(resp.WorkerID, "wrong-secret") {
		t.Error("CheckSecret(id, wrong) = true, want false")
	}
}

func TestHandleRegisterBodyTooLarge(t *testing.T) {
	reg := registry.New(nil)
	s := &Server{reg: reg, log: slog.Default()}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/workers/register", s.handleRegister)

	// Build a body larger than 1 MB.
	large := strings.Repeat("a", 1<<20+1) // 1 MB + 1 byte
	req := httptest.NewRequest(http.MethodPost, "/v1/workers/register", strings.NewReader(large))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413 for oversized body", rec.Code)
	}
}

// TestHandleRegisterBodyTooLargeIntegration verifies that a real http.Server
// returns 413 for oversized bodies (http.MaxBytesReader behavior). The unit
// test above uses ServeHTTP directly, which surfaces the read error as 400
// instead of 413.
func TestHandleRegisterBodyTooLargeIntegration(t *testing.T) {
	reg := registry.New(nil)
	s := &Server{reg: reg, log: slog.Default()}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/workers/register", s.handleRegister)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	large := strings.Repeat("a", 1<<20+1) // 1 MB + 1 byte
	resp, err := srv.Client().Post(srv.URL+"/v1/workers/register", "application/json", strings.NewReader(large))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413 for oversized body", resp.StatusCode)
	}
}

// --- Step 1.4: authenticated heartbeat + worker list ---

func hexHash(s string) [32]byte { return registry.HashSecret(s) }

func TestRequireWorkerStatusContract(t *testing.T) {
	reg := registry.New(nil)
	s := &Server{reg: reg, log: slog.Default()}

	// Register a known worker with a known secret.
	reg.Register("wrk_test", "host-a", "darwin", "amd64", "", 4, 8_000_000_000, hexHash("s3cret"))

	// Build a handler that just records it was called.
	var called bool
	next := func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(http.StatusOK) }

	tests := []struct {
		name       string
		id         string // worker id in path
		authHeader string // Authorization header value ("" = missing)
		wantStatus int
	}{
		{"unknown worker -> 404", "wrk_unknown", "Bearer s3cret", http.StatusNotFound},
		{"missing auth header -> 401", "wrk_test", "", http.StatusUnauthorized},
		{"wrong scheme (Basic) -> 401", "wrk_test", "Basic s3cret", http.StatusUnauthorized},
		{"wrong secret -> 401", "wrk_test", "Bearer wrong-secret", http.StatusUnauthorized},
		{"empty bearer token -> 401", "wrk_test", "Bearer ", http.StatusUnauthorized},
		{"empty bearer token no space -> 401", "wrk_test", "Bearer", http.StatusUnauthorized},
		{"correct secret -> 200", "wrk_test", "Bearer s3cret", http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called = false
			req := httptest.NewRequest(http.MethodPost, "/v1/workers/"+tc.id+"/heartbeat", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			// Set the path value via a real mux so r.PathValue works.
			mux := http.NewServeMux()
			mux.HandleFunc("POST /v1/workers/{id}/heartbeat", s.requireWorker(next))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantStatus == http.StatusOK && !called {
				t.Error("next handler was not called on auth success")
			}
			if tc.wantStatus != http.StatusOK && called {
				t.Error("next handler was called despite auth failure")
			}
		})
	}
}

func TestRequireWorkerChecks404Before401(t *testing.T) {
	reg := registry.New(nil)
	s := &Server{reg: reg, log: slog.Default()}
	// No workers registered.

	next := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/workers/{id}/heartbeat", s.requireWorker(next))

	// Unknown worker + NO auth header: should be 404, NOT 401.
	// (Don't reveal whether a worker id exists via auth error.)
	req := httptest.NewRequest(http.MethodPost, "/v1/workers/wrk_nope/heartbeat", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown worker + no auth = %d, want 404 (checked before 401)", rec.Code)
	}
}

func TestHandleHeartbeatFullFlow(t *testing.T) {
	reg := registry.New(nil)
	reg.Register("wrk_test", "host-a", "darwin", "amd64", "", 4, 8_000_000_000, hexHash("s3cret"))

	h := NewHandler(reg) // full mux with requireWorker + routes

	// Valid authenticated heartbeat through the real route.
	body := `{"worker_id":"wrk_test","seq":1,"agent_version":"0.1.0","cpu_percent":5,"ram_total_bytes":8000000000,"ram_available_bytes":4000000000,"active_tasks":0,"loaded_models":[]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/workers/wrk_test/heartbeat", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer s3cret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	// Response is api.HeartbeatResponse with NextHeartbeatSeconds=5, MinAgentVersion="0.1.0".
	var hbResp api.HeartbeatResponse
	if err := json.NewDecoder(rec.Body).Decode(&hbResp); err != nil {
		t.Fatalf("decode heartbeat response: %v", err)
	}
	if hbResp.NextHeartbeatSeconds != 5 {
		t.Errorf("NextHeartbeatSeconds = %d, want 5", hbResp.NextHeartbeatSeconds)
	}
	if hbResp.MinAgentVersion != "0.1.0" {
		t.Errorf("MinAgentVersion = %q, want %q", hbResp.MinAgentVersion, "0.1.0")
	}

	// GET /v1/workers shows the worker online with right hostname and last.cpu_percent.
	listReq := httptest.NewRequest(http.MethodGet, "/v1/workers", nil)
	listRec := httptest.NewRecorder()
	h.ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", listRec.Code)
	}
	var workers []*registry.View
	if err := json.NewDecoder(listRec.Body).Decode(&workers); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(workers) != 1 {
		t.Fatalf("list has %d workers, want 1", len(workers))
	}
	if workers[0].Hostname != "host-a" {
		t.Errorf("hostname = %q, want %q", workers[0].Hostname, "host-a")
	}
	if workers[0].Status != "online" {
		t.Errorf("status = %q, want %q", workers[0].Status, "online")
	}
	if workers[0].Last.CPUPercent != 5 {
		t.Errorf("last.cpu_percent = %v, want 5", workers[0].Last.CPUPercent)
	}
}

func TestHandleListWorkersSecurity(t *testing.T) {
	reg := registry.New(nil)
	s := &Server{reg: reg, log: slog.Default()}
	reg.Register("wrk_test", "host-a", "darwin", "amd64", "", 4, 8_000_000_000, hexHash("s3cret"))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/workers", s.handleListWorkers)

	req := httptest.NewRequest(http.MethodGet, "/v1/workers", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	// No secret and no hash anywhere in the JSON.
	lower := strings.ToLower(body)
	if strings.Contains(lower, "secret") {
		t.Errorf("list response contains 'secret': %s", body)
	}
	if strings.Contains(lower, "hash") {
		t.Errorf("list response contains 'hash': %s", body)
	}
	// snake_case JSON tags on View.
	for _, k := range []string{`"id"`, `"hostname"`, `"os"`, `"arch"`, `"cpu_model"`, `"cpu_cores"`, `"ram_total_bytes"`, `"labels"`, `"last_seen"`, `"status"`, `"last"`} {
		if !strings.Contains(body, k) {
			t.Errorf("list response missing key %s in %s", k, body)
		}
	}
}

func TestHandleListWorkersEmptyReturnsNotNull(t *testing.T) {
	reg := registry.New(nil)
	s := &Server{reg: reg, log: slog.Default()}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/workers", s.handleListWorkers)

	req := httptest.NewRequest(http.MethodGet, "/v1/workers", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := strings.TrimSpace(rec.Body.String())
	if body != "[]" {
		t.Errorf("empty registry returned %q, want %q (not null)", body, "[]")
	}
}

func TestWorkerStatusWithFakeClock(t *testing.T) {
	// Deterministic fake clock injected into the registry.
	var mu sync.Mutex
	var current time.Time = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	now := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return current
	}
	advance := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		current = current.Add(d)
	}

	reg := registry.New(now)
	reg.Register("wrk_test", "host-a", "darwin", "amd64", "", 4, 8_000_000_000, hexHash("s3cret"))

	h := NewHandler(reg)

	// Send a heartbeat to stamp LastSeen at current time.
	body := `{"worker_id":"wrk_test","seq":1,"agent_version":"0.1.0","cpu_percent":5,"ram_total_bytes":8000000000,"ram_available_bytes":4000000000,"active_tasks":0,"loaded_models":[]}`
	hbReq := httptest.NewRequest(http.MethodPost, "/v1/workers/wrk_test/heartbeat", strings.NewReader(body))
	hbReq.Header.Set("Authorization", "Bearer s3cret")
	hbRec := httptest.NewRecorder()
	h.ServeHTTP(hbRec, hbReq)
	if hbRec.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d, want 200", hbRec.Code)
	}

	getStatus := func() string {
		req := httptest.NewRequest(http.MethodGet, "/v1/workers", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var workers []*registry.View
		if err := json.NewDecoder(rec.Body).Decode(&workers); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		if len(workers) != 1 {
			t.Fatalf("got %d workers, want 1", len(workers))
		}
		return workers[0].Status
	}

	// online immediately after heartbeat.
	if got := getStatus(); got != "online" {
		t.Errorf("at 0s: status = %q, want %q", got, "online")
	}

	// advance 20s -> stale.
	advance(20 * time.Second)
	if got := getStatus(); got != "stale" {
		t.Errorf("at 20s: status = %q, want %q", got, "stale")
	}

	// advance to 50s total -> offline.
	advance(30 * time.Second)
	if got := getStatus(); got != "offline" {
		t.Errorf("at 50s: status = %q, want %q", got, "offline")
	}
}

// Pre-work (Step 1.5): cpu_model must be a STRING, not 0.
func TestCPUModelIsString(t *testing.T) {
	reg := registry.New(nil)
	h := NewHandler(reg)

	// Register with real specs, CPU model "Test CPU".
	specsJSON := `{"hostname":"test-machine","os":"darwin","arch":"amd64","cpu":"Test CPU","cores":4,"total_ram":8000000000}`
	req := httptest.NewRequest(http.MethodPost, "/v1/workers/register", strings.NewReader(`{"join_token":"dev","specs":`+specsJSON+`}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want 201", rec.Code)
	}

	// GET /v1/workers must contain "cpu_model":"Test CPU" (string), not 0.
	listReq := httptest.NewRequest(http.MethodGet, "/v1/workers", nil)
	listRec := httptest.NewRecorder()
	h.ServeHTTP(listRec, listReq)
	body := listRec.Body.String()
	if !strings.Contains(body, `"cpu_model":"Test CPU"`) {
		t.Errorf("GET /v1/workers missing string cpu_model \"Test CPU\": %s", body)
	}
	if strings.Contains(body, `"cpu_model":0`) {
		t.Errorf("cpu_model is numeric 0, want string \"Test CPU\": %s", body)
	}
	// Ensure JSON type is string, not number.
	var workers []map[string]any
	if err := json.Unmarshal([]byte(body), &workers); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(workers) != 1 {
		t.Fatalf("got %d workers, want 1", len(workers))
	}
	if _, ok := workers[0]["cpu_model"].(string); !ok {
		t.Errorf("cpu_model is %T (%v), want string", workers[0]["cpu_model"], workers[0]["cpu_model"])
	}
}
