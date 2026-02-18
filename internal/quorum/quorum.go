package quorum

import "github.com/tmater/wacht/internal/proto"

// consecutiveFailureThreshold is the number of consecutive down results required
// from a single probe before it is considered to be observing a real outage.
const consecutiveFailureThreshold = 2

// MajorityDown returns true if a strict majority of probes report the check as down.
// Each result in results should be the most recent result for a distinct probe.
func MajorityDown(results []proto.CheckResult) bool {
	if len(results) == 0 {
		return false
	}
	down := 0
	for _, r := range results {
		if !r.Up {
			down++
		}
	}
	return down > len(results)/2
}

// AllConsecutivelyDown returns true if every result in the slice is down.
// Pass the last N results for a single probe, newest first.
// Returns false if fewer than consecutiveFailureThreshold results are provided.
func AllConsecutivelyDown(results []proto.CheckResult) bool {
	if len(results) < consecutiveFailureThreshold {
		return false
	}
	for _, r := range results {
		if r.Up {
			return false
		}
	}
	return true
}
