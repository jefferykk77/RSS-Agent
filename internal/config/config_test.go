package config

import (
	"path/filepath"
	"testing"
)

func TestCandidateLimitDefaultsAndAllowsZero(t *testing.T) {
	if got := (Settings{}).CandidateLimit(); got != 24 {
		t.Fatalf("default candidate limit = %d, want 24", got)
	}
	unlimited := 0
	if got := (Settings{MaxCandidatesPerRun: &unlimited}).CandidateLimit(); got != 0 {
		t.Fatalf("explicit candidate limit = %d, want 0", got)
	}
}

func TestProfileHashIncludesModelAndPromptVersion(t *testing.T) {
	cfg := Sample()
	base := cfg.ProfileHash()
	cfg.Models.Primary.Name = "different-endpoint"
	if cfg.ProfileHash() == base {
		t.Fatal("ProfileHash must change when the model endpoint changes")
	}
	modelHash := cfg.ProfileHash()
	cfg.Settings.AnalysisPromptVersion = "next-prompt"
	if cfg.ProfileHash() == modelHash {
		t.Fatal("ProfileHash must change when the prompt version changes")
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

func TestPushConfigResolvesChannelSecrets(t *testing.T) {
	cfg := Sample()
	t.Setenv("FEISHU_WEBHOOK_URL", "https://feishu.example/webhook")
	t.Setenv("DINGTALK_WEBHOOK_URL", "https://dingtalk.example/webhook")
	t.Setenv("TELEGRAM_BOT_TOKEN", "telegram-token")
	t.Setenv("TELEGRAM_CHAT_ID", "telegram-chat")
	t.Setenv("RSS_AGENT_SMTP_USERNAME", "rss@example.com")
	t.Setenv("RSS_AGENT_SMTP_PASSWORD", "smtp-password")

	if got := cfg.FeishuWebhookURL(); got != "https://feishu.example/webhook" {
		t.Fatalf("FeishuWebhookURL() = %q", got)
	}
	if got := cfg.DingTalkWebhookURL(); got != "https://dingtalk.example/webhook" {
		t.Fatalf("DingTalkWebhookURL() = %q", got)
	}
	if got := cfg.TelegramBotToken(); got != "telegram-token" {
		t.Fatalf("TelegramBotToken() = %q", got)
	}
	if got := cfg.TelegramChatID(); got != "telegram-chat" {
		t.Fatalf("TelegramChatID() = %q", got)
	}
	if got := cfg.EmailUsername(); got != "rss@example.com" {
		t.Fatalf("EmailUsername() = %q", got)
	}
	if got := cfg.EmailPassword(); got != "smtp-password" {
		t.Fatalf("EmailPassword() = %q", got)
	}
	if !cfg.EmailStartTLS() {
		t.Fatal("EmailStartTLS() = false, want true")
	}
}

func TestPushConfigValidation(t *testing.T) {
	cfg := Sample()
	cfg.Push.Telegram.BotToken = "token-only"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil for incomplete Telegram configuration")
	}

	cfg = Sample()
	cfg.Push.Email.SMTPHost = "smtp.example.com"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil for email without recipients")
	}
}

func TestSampleRoundTripsThroughYAML(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TELEGRAM_CHAT_ID", "")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, Sample()); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestXSearchDefaultsAndTokenResolution(t *testing.T) {
	cfg := Sample()
	cfg.X.Searches = []XSearch{{Name: "X AI", Query: "AI lang:en -is:retweet"}}
	cfg.ApplyDefaults()
	if cfg.X.BaseURL != "https://api.x.com/2" || cfg.X.Searches[0].MaxResults != 20 {
		t.Fatalf("X defaults = %+v", cfg.X)
	}
	t.Setenv("X_BEARER_TOKEN", "x-token")
	if got := cfg.XBearerToken(); got != "x-token" {
		t.Fatalf("XBearerToken() = %q", got)
	}
	if searches := cfg.EnabledXSearches(); len(searches) != 1 || searches[0].Name != "X AI" {
		t.Fatalf("EnabledXSearches() = %+v", searches)
	}
	cfg.X.Searches[0].MaxResults = 9
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil for invalid X search max_results")
	}
}
