package registry

import "time"

const (
	StaleAfter   = 15 * time.Second
	OfflineAfter = 45 * time.Second
)

func StatusFor(lastSeen, now time.Time) string {
	if lastSeen.IsZero() {
		return "pending"
	}
	age := now.Sub(lastSeen)
	switch {
	case age < StaleAfter:
		return "online"
	case age < OfflineAfter:
		return "stale"
	default:
		return "offline"
	}
}
