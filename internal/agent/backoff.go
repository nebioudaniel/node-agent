package agent

import "time"

// Backoff returns how long to wait before retry attempt `attempt`
// (0-based). The exponential cap d = base * 2^attempt is clamped to max
// and attempt is clamped (e.g. [0,30]) so the shift never overflows
// time.Duration (int64). Final delay = d/2 + rnd()*d/2, i.e. uniformly in
// [d/2, d), always within [0, max). rnd returns a float in [0,1) (like
// rand.Float64); it is injected so tests are deterministic and need no
// sleeping or real randomness.
//
// Rules:
//   - d = base * 2^attempt, capped at max
//   - clamp attempt (negative -> 0; huge -> e.g. 30) so `1<<attempt` can't
//     overflow int64 time.Duration; never panic on negative shift
//   - result = d/2 + rnd()*d/2 ; never negative; never exceeds max
func Backoff(attempt int, base, max time.Duration, rnd func() float64) time.Duration {
	if attempt < 0 {
		attempt = 0 // avoid negative shift count panic + go negative
	}
	if attempt > 30 {
		attempt = 30 // keep 1<<attempt small; can't overflow int64 anyway once capped
	}

	if base < 0 {
		base = 0
	}
	if max < 0 {
		max = 0
	}

	// d = base * 2^attempt with overflow protection.
	d := base
	if attempt > 0 {
		shifted := base << attempt
		// If shifting wrapped (overflowed int64), clamp to max.
		if shifted < base || shifted < 0 {
			shifted = max
		}
		d = shifted
	}

	if d < 0 {
		d = max
	}
	if d > max {
		d = max // cap exponential value
	}

	half := d / 2

	r := 0.0
	if rnd != nil {
		r = rnd()
	}
	// rnd returns [0,1); clamp defensively so output stays in [d/2, d).
	if r < 0 {
		r = 0
	}
	if r >= 1 {
		r = 0.9999999999999999
	}

	jitter := time.Duration(r * float64(half))
	result := half + jitter

	if result < 0 {
		return 0
	}
	if result > max {
		return max
	}
	return result
}
