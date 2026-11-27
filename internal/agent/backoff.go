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
// Rules to implement:
//   - d = base * 2^attempt, capped at max
//   - clamp attempt (negative -> 0; huge -> e.g. 30) so `1<<attempt` can't
//     overflow int64 time.Duration; never panic on negative shift
//   - result = d/2 + rnd()*d/2 ; never negative; never exceeds max
func Backoff(attempt int, base, max time.Duration, rnd func() float64) time.Duration {
	// TODO: implement per rules above.
	_ = attempt
	_ = base
	_ = max
	_ = rnd
	return 0
}
