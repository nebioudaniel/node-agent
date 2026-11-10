package api

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// validHeartbeat returns a heartbeat that passes Validate.
func validHeartbeat() Heartbeat {
	return Heartbeat{
		WorkerID:          "worker-1",
		Seq:               42,
		SentAt:            time.Date(2026, 11, 10, 12, 30, 45, 0, time.UTC),
		AgentVersion:      "1.2.3",
		CPUPercent:        37.5,
		RAMTotalBytes:     16 << 30,
		RAMAvailableBytes: 8 << 30,
		ActiveTasks:       3,
		LoadedModels:      []string{"llama3", "mistral"},
	}
}

func TestHeartbeatValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(h *Heartbeat)
		wantErr bool
	}{
		{"valid", func(h *Heartbeat) {}, false},
		{"empty worker id", func(h *Heartbeat) { h.WorkerID = "" }, true},
		{"zero ram total", func(h *Heartbeat) { h.RAMTotalBytes = 0; h.RAMAvailableBytes = 0 }, true},
		{"ram available > total", func(h *Heartbeat) { h.RAMAvailableBytes = h.RAMTotalBytes + 1 }, true},
		{"cpu below 0", func(h *Heartbeat) { h.CPUPercent = -0.1 }, true},
		{"cpu above 100", func(h *Heartbeat) { h.CPUPercent = 100.1 }, true},
		{"negative active tasks", func(h *Heartbeat) { h.ActiveTasks = -1 }, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := validHeartbeat()
			tc.mutate(&h)
			err := h.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestHeartbeatJSONRoundTrip(t *testing.T) {
	orig := validHeartbeat()

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Every field must be present under its snake_case key.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}
	wantKeys := []string{
		"worker_id", "seq", "sent_at", "agent_version", "cpu_percent",
		"ram_total_bytes", "ram_available_bytes", "active_tasks", "loaded_models",
	}
	for _, k := range wantKeys {
		if _, ok := raw[k]; !ok {
			t.Errorf("missing JSON key %q in %s", k, data)
		}
	}
	if len(raw) != len(wantKeys) {
		t.Errorf("got %d JSON keys, want %d: %s", len(raw), len(wantKeys), data)
	}

	var got Heartbeat
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// time.Time needs Equal, not DeepEqual, so compare it separately.
	if !got.SentAt.Equal(orig.SentAt) {
		t.Errorf("SentAt = %v, want %v", got.SentAt, orig.SentAt)
	}
	got.SentAt, orig.SentAt = time.Time{}, time.Time{}
	if !reflect.DeepEqual(got, orig) {
		t.Errorf("round trip mismatch:\n got  %+v\n want %+v", got, orig)
	}
}

func TestResponsesJSONRoundTrip(t *testing.T) {
	reg := RegisterResponse{WorkerID: "worker-1", Secret: "s3cret", HeartbeatSeconds: 10}
	data, err := json.Marshal(reg)
	if err != nil {
		t.Fatalf("marshal RegisterResponse: %v", err)
	}
	var regGot RegisterResponse
	if err := json.Unmarshal(data, &regGot); err != nil {
		t.Fatalf("unmarshal RegisterResponse: %v", err)
	}
	if regGot != reg {
		t.Errorf("RegisterResponse = %+v, want %+v", regGot, reg)
	}

	hbr := HeartbeatResponse{NextHeartbeatSeconds: 15, MinAgentVersion: "1.0.0"}
	data, err = json.Marshal(hbr)
	if err != nil {
		t.Fatalf("marshal HeartbeatResponse: %v", err)
	}
	var hbrGot HeartbeatResponse
	if err := json.Unmarshal(data, &hbrGot); err != nil {
		t.Fatalf("unmarshal HeartbeatResponse: %v", err)
	}
	if hbrGot != hbr {
		t.Errorf("HeartbeatResponse = %+v, want %+v", hbrGot, hbr)
	}
}
