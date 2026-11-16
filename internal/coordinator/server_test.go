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
	"time"

	"node-agent/internal/api"
	"node-agent/internal/coordinator/registry"
)

const validBody = `{"worker_id":"wrk_test","seq":1,"agent_version":"0.1.0","cpu_percent":10,"ram_total_bytes":8000000000,"ram_available_bytes":4000000000,"active_tasks":0,"loaded_models":[]}`

const validRegisterBody = `{"join_token":"dev","specs":{"hostname":"lab-a","os":"linux","arch":"amd64","cpu":"test cpu","cores":8,"total_ram":17179869184}}`

// newTestHandler builds a handler backed by a registry with a fixed clock.
func newTestHandler(t *testing.T) (http.Handler, *registry.Registry) {
	t.Helper()
	reg := registry.New(func() time.Time {
		return time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewHandler(reg, log), reg
}

func TestHeartbeatHandler(t *testing.T) {
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

	h, _ := newTestHandler(t)
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

func TestRegisterHandler(t *testing.T) {
	h, reg := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/workers/register", strings.NewReader(validRegisterBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var resp api.RegisterResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body: %s)", err, rec.Body.String())
	}

	if !strings.HasPrefix(resp.WorkerID, "wrk_") {
		t.Errorf("worker_id = %q, want prefix wrk_", resp.WorkerID)
	}
	if len(resp.WorkerID) != len("wrk_")+12 {
		t.Errorf("worker_id = %q, want 6 random bytes in hex after wrk_", resp.WorkerID)
	}
	if len(resp.Secret) != 64 {
		t.Errorf("secret length = %d, want 64 hex chars", len(resp.Secret))
	}
	if _, err := hex.DecodeString(resp.Secret); err != nil {
		t.Errorf("secret is not hex: %v", err)
	}
	if resp.HeartbeatSeconds != 5 {
		t.Errorf("heartbeat_seconds = %d, want 5", resp.HeartbeatSeconds)
	}

	// The worker is stored, its secret hash matches, and it starts pending.
	v, ok := reg.Get(resp.WorkerID)
	if !ok {
		t.Fatalf("worker %q not in registry after register", resp.WorkerID)
	}
	if v.Status != "pending" {
		t.Errorf("status = %q, want pending before the first heartbeat", v.Status)
	}
	if v.Hostname != "lab-a" || v.CPUModel != "test cpu" || v.CPUCores != 8 || v.RAMTotalBytes != 17179869184 {
		t.Errorf("specs not mapped onto the worker: %+v", v)
	}
	if !reg.CheckSecret(resp.WorkerID, resp.Secret) {
		t.Error("CheckSecret returned false for the secret handed out at register")
	}
	if reg.CheckSecret(resp.WorkerID, "wrong-secret") {
		t.Error("CheckSecret returned true for a wrong secret")
	}

	// The secret must not leak through the read API.
	list := reg.List()
	if strings.Contains(rec.Body.String(), "secret_hash") || len(list) != 1 {
		t.Errorf("unexpected registry contents after register: %+v", list)
	}
}

func TestRegisterHandlerBadRequests(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"not json", `not json`, http.StatusBadRequest},
		{"empty body", ``, http.StatusBadRequest},
		{"too large", `{"join_token":"` + strings.Repeat("a", maxBodyBytes) + `"}`, http.StatusRequestEntityTooLarge},
	}

	h, reg := newTestHandler(t)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/workers/register", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}

	if got := len(reg.List()); got != 0 {
		t.Errorf("registry has %d workers after only bad requests, want 0", got)
	}
}

func TestNewWorkerIDAndSecret(t *testing.T) {
	id1, err := newWorkerID()
	if err != nil {
		t.Fatalf("newWorkerID: %v", err)
	}
	id2, err := newWorkerID()
	if err != nil {
		t.Fatalf("newWorkerID: %v", err)
	}
	if id1 == id2 {
		t.Errorf("newWorkerID returned the same id twice: %q", id1)
	}

	secret, err := newSecret()
	if err != nil {
		t.Fatalf("newSecret: %v", err)
	}
	if len(secret) != 64 {
		t.Errorf("secret length = %d, want 64", len(secret))
	}
}

func TestNewHandlerNilLogger(t *testing.T) {
	reg := registry.New(time.Now)
	h := NewHandler(reg, nil)
	if h == nil {
		t.Fatal("NewHandler returned nil")
	}
}
