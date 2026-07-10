package config

import "testing"

func TestCandidateLimitDefaultsAndAllowsZero(t *testing.T) {
	if got := (Settings{}).CandidateLimit(); got != 24 {
		t.Fatalf("default candidate limit = %d, want 24", got)
	}
	unlimited := 0
	if got := (Settings{MaxCandidatesPerRun: &unlimited}).CandidateLimit(); got != 0 {
		t.Fatalf("explicit candidate limit = %d, want 0", got)
	}
}
