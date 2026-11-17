package coordinator

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const validBody = `{"worker_id":"wrk_test","seq":1,"agent_version":"0.1.0","cpu_percent":10,"ram_total_bytes":8000000000,"ram_available_bytes":4000000000,"active_tasks":0,"loaded_models":[]}`

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

	h := NewHandler()
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
