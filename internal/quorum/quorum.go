package quorum

import "github.com/tmater/wacht/internal/proto"

// Evaluate returns true if at least `threshold` probes are currently reporting
// the check as down. Each result in `results` should be the most recent result
// for a distinct probe (i.e. one entry per probe).
func Evaluate(results []proto.CheckResult, threshold int) bool {
	if len(results) == 0 {
		return false
	}
	down := 0
	for _, r := range results {
		if !r.Up {
			down++
		}
	}
	return down >= threshold
}
