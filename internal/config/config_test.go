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

func TestFullTextDefaultsAndValidation(t *testing.T) {
	cfg := Sample()
	if cfg.Settings.FullTextMinChars != 600 {
		t.Fatalf("full text minimum = %d, want 600", cfg.Settings.FullTextMinChars)
	}
	if cfg.Settings.FullTextMaxChars != 8000 {
		t.Fatalf("full text maximum = %d, want 8000", cfg.Settings.FullTextMaxChars)
	}

	cfg.Settings.FullTextMinChars = 8001
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want invalid full text range error")
	}
}
