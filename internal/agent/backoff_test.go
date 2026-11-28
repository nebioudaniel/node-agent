package agent

import (
	"testing"
	"time"
)

func rnd0() float64      { return 0 }
func rnd999999() float64 { return 0.999999 }

// rnd0 gives exactly d/2; rnd0.999999 is just under d.
func TestBackoffJitterRange(t *testing.T) {
	base := time.Second
	max := 30 * time.Second
	const attempt = 0
	d := time.Second // base*2^0

	if got := Backoff(attempt, base, max, rnd0); got != d/2 {
		t.Errorf("rnd=0: got %v, want exactly d/2=%v", got, d/2)
	}
	// must not pass trivially when Backoff returns 0:
	if got := Backoff(attempt, base, max, rnd0); got <= 0 && base > 0 {
		t.Errorf("rnd=0: got %v, want > 0 for base > 0", got)
	}

	got := Backoff(attempt, base, max, rnd999999)
	if got <= d/2 {
		t.Errorf("rnd=0.999999: got %v, want just under d=%v (and > d/2=%v)", got, d, d/2)
	}
	if got >= d {
		t.Errorf("rnd=0.999999: got %v, want strictly < d=%v", got, d)
	}
}

// Growth table: base=1s, max=30s, attempts 0..5, rnd=0 -> d/2.
// d = 1s*2^attempt capped at 30s from attempt 5 on; here 2^5=32s caps to 30s.
func TestBackoffGrowthTable(t *testing.T) {
	base := time.Second
	max := 30 * time.Second

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 500 * time.Millisecond}, // d=1s  -> 0.5s
		{1, 1 * time.Second},        // d=2s  -> 1s
		{2, 2 * time.Second},        // d=4s  -> 2s
		{3, 4 * time.Second},        // d=8s  -> 4s
		{4, 8 * time.Second},        // d=16s -> 8s
		{5, 15 * time.Second},       // d=32s cap 30s -> 15s
	}

	for _, tc := range tests {
		if got := Backoff(tc.attempt, base, max, rnd0); got != tc.want {
			t.Errorf("attempt=%d rnd=0: got %v, want %v", tc.attempt, got, tc.want)
		}
		if got := Backoff(tc.attempt, base, max, rnd0); got <= 0 && base > 0 {
			t.Errorf("attempt=%d: got %v, want > 0 for base > 0", tc.attempt, got)
		}
	}
}

// attempts 0..100 with several rnd values: result never exceeds max, never negative.
func TestBackoffNeverExceedsMaxNeverNegative(t *testing.T) {
	base := time.Second
	max := 30 * time.Second

	rnds := map[string]func() float64{
		"rnd=0":        func() float64 { return 0 },
		"rnd=0.25":     func() float64 { return 0.25 },
		"rnd=0.5":      func() float64 { return 0.5 },
		"rnd=0.75":     func() float64 { return 0.75 },
		"rnd=0.999999": func() float64 { return 0.999999 },
	}

	for name, rnd := range rnds {
		for attempt := 0; attempt <= 100; attempt++ {
			got := Backoff(attempt, base, max, rnd)
			if got < 0 {
				t.Fatalf("%s attempt=%d: got %v, negative", name, attempt, got)
			}
			if got > max {
				t.Fatalf("%s attempt=%d: got %v, exceeds max=%v", name, attempt, got, max)
			}
		}
	}
}

// negative attempt does not panic or go negative.
func TestBackoffNegativeAttempt(t *testing.T) {
	base := time.Second
	max := 30 * time.Second

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Backoff panicked on negative attempt: %v", r)
		}
	}()

	got := Backoff(-1, base, max, rnd0)
	if got < 0 {
		t.Errorf("negative attempt: got %v, negative", got)
	}
	if got > max {
		t.Errorf("negative attempt: got %v, exceeds max=%v", got, max)
	}
}
