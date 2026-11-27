package coordinator

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"node-agent/internal/api"
	"node-agent/internal/coordinator/registry"
	"node-agent/internal/specs"
)

const validBody = `{"worker_id":"wrk_test","seq":1,"agent_version":"0.1.0","cpu_percent":10,"ram_total_bytes":8000000000,"ram_available_bytes":4000000000,"active_tasks":0,"loaded_models":[]}`

func TestHeartbeatHandler(t *testing.T) {
	reg := registry.New(nil)
	h := NewHandler(reg)
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/workers/wrk_test/heartbeat", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

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

	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 400 or 413 for oversized body", rec.Code)
	}
}
