package registry

import (
	"testing"
	"time"
)

func TestStatusFor(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		lastSeen time.Time
		want     string
	}{
		{"zero time", time.Time{}, "pending"},
		{"0s", now, "online"},
		{"14.9s", now.Add(-14900 * time.Millisecond), "online"},
		{"15s", now.Add(-15 * time.Second), "stale"},
		{"44.9s", now.Add(-44900 * time.Millisecond), "stale"},
		{"45s", now.Add(-45 * time.Second), "offline"},
		{"10min", now.Add(-10 * time.Minute), "offline"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := StatusFor(tc.lastSeen, now)
			if got != tc.want {
				t.Errorf("StatusFor(%v, now) = %q, want %q", tc.lastSeen, got, tc.want)
			}
		})
	}
}
