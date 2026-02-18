package quorum

import (
	"testing"

	"github.com/tmater/wacht/internal/proto"
)

func results(ups ...bool) []proto.CheckResult {
	var rs []proto.CheckResult
	for _, up := range ups {
		rs = append(rs, proto.CheckResult{Up: up})
	}
	return rs
}

func TestMajorityDown(t *testing.T) {
	tests := []struct {
		name string
		ups  []bool
		want bool
	}{
		{"empty", []bool{}, false},
		{"single up", []bool{true}, false},
		{"single down", []bool{false}, true},
		{"two up one down", []bool{true, true, false}, false},
		{"two down one up", []bool{false, false, true}, true},
		{"exactly half down — no majority", []bool{false, false, true, true}, false},
		{"all down", []bool{false, false, false}, true},
		{"all up", []bool{true, true, true}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MajorityDown(results(tt.ups...))
			if got != tt.want {
				t.Errorf("MajorityDown(%v) = %v, want %v", tt.ups, got, tt.want)
			}
		})
	}
}

func TestAllConsecutivelyDown(t *testing.T) {
	tests := []struct {
		name string
		ups  []bool
		want bool
	}{
		{"empty", []bool{}, false},
		{"one down — below threshold", []bool{false}, false},
		{"two down — meets threshold", []bool{false, false}, true},
		{"three down", []bool{false, false, false}, true},
		{"two down one up", []bool{false, false, true}, false},
		{"one up then two down — up breaks streak", []bool{false, true, false}, false},
		{"all up", []bool{true, true}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AllConsecutivelyDown(results(tt.ups...))
			if got != tt.want {
				t.Errorf("AllConsecutivelyDown(%v) = %v, want %v", tt.ups, got, tt.want)
			}
		})
	}
}
