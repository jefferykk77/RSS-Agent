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

func TestResolveProfileKeepsScopedConfigAndInheritedModels(t *testing.T) {
	cfg := Sample()
	product, err := cfg.ResolveProfile("product")
	if err != nil {
		t.Fatalf("ResolveProfile(product) error = %v", err)
	}
	if len(product.Profile.Interests) != 1 || product.Profile.Interests[0] != "AI 产品策略、开发者工具、产品增长与竞品动态" {
		t.Fatalf("product interests = %+v", product.Profile.Interests)
	}
	if len(product.Feeds) != 1 || product.Feeds[0].Name != "Go Blog" {
		t.Fatalf("product feeds = %+v", product.Feeds)
	}
	if product.Models.Primary.Name != "${ARK_MODEL}" {
		t.Fatalf("inherited model = %+v", product.Models.Primary)
	}
	if names := cfg.ProfileNames(); len(names) != 2 || names[0] != DefaultProfileName || names[1] != "product" {
		t.Fatalf("profile names = %+v", names)
	}
	if _, err := cfg.ResolveProfile("missing"); err == nil {
		t.Fatal("ResolveProfile(missing) error = nil")
	}
}
