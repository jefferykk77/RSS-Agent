package triage

import (
	"testing"
	"time"

	"github.com/jeffery/rss-agent/internal/config"
	"github.com/jeffery/rss-agent/internal/rss"
)

func TestFilterAppliesRulesAndRanksPriority(t *testing.T) {
	now := time.Date(2026, time.July, 10, 10, 0, 0, 0, time.UTC)
	items := []rss.Item{
		{ID: "priority", Title: "Eino release", FeedTags: []string{"go"}, PublishedAt: now.Add(-2 * time.Hour)},
		{ID: "recent", Title: "General update", PublishedAt: now.Add(-time.Hour)},
		{ID: "muted", Title: "Eino daily", FeedName: "Muted source", PublishedAt: now.Add(-time.Hour)},
		{ID: "excluded", Title: "Funding news", PublishedAt: now.Add(-time.Hour)},
		{ID: "required", Title: "Other news", PublishedAt: now.Add(-time.Hour)},
		{ID: "old", Title: "Eino old", PublishedAt: now.Add(-48 * time.Hour)},
		{ID: "priority", Title: "Duplicate", PublishedAt: now.Add(-time.Hour)},
	}
	profile := config.Profile{
		PriorityTerms: []string{"Eino"},
		MustInclude:   []string{"update", "release"},
		Exclude:       []string{"funding"},
		MutedFeeds:    []string{"Muted source"},
	}
	limit := 5
	settings := config.Settings{LookbackHours: 24, MaxCandidatesPerRun: &limit}

	got := Filter(items, map[string]bool{"recent": true}, profile, settings, false, now)
	if len(got.Items) != 1 || got.Items[0].ID != "priority" {
		t.Fatalf("items = %#v, want only priority", got.Items)
	}
	if got.Items[0].LocalScore != 5 || len(got.Items[0].LocalReasons) != 1 {
		t.Fatalf("local hints = %#v", got.Items[0])
	}
	if got.Stats.Seen != 1 || got.Stats.Muted != 1 || got.Stats.Excluded != 1 || got.Stats.MissingRequired != 1 || got.Stats.Stale != 1 || got.Stats.Duplicate != 1 {
		t.Fatalf("stats = %#v", got.Stats)
	}
}

func TestFilterCapsAfterPrioritySort(t *testing.T) {
	now := time.Date(2026, time.July, 10, 10, 0, 0, 0, time.UTC)
	items := []rss.Item{
		{ID: "normal", Title: "Update", PublishedAt: now.Add(-time.Hour)},
		{ID: "priority", Title: "Eino update", PublishedAt: now.Add(-2 * time.Hour)},
	}
	limit := 1
	got := Filter(items, nil, config.Profile{PriorityTerms: []string{"Eino"}}, config.Settings{LookbackHours: 24, MaxCandidatesPerRun: &limit}, false, now)
	if len(got.Items) != 1 || got.Items[0].ID != "priority" || got.Stats.Capped != 1 {
		t.Fatalf("result = %#v", got)
	}
}

func TestFilterDoesNotMatchShortASCIITermsInsideWords(t *testing.T) {
	now := time.Date(2026, time.July, 10, 10, 0, 0, 0, time.UTC)
	items := []rss.Item{
		{ID: "not-go", Title: "Algorithms update", PublishedAt: now.Add(-time.Hour)},
		{ID: "go", Title: "Go update", PublishedAt: now.Add(-2 * time.Hour)},
	}
	got := Filter(items, nil, config.Profile{PriorityTerms: []string{"Go"}}, config.Settings{LookbackHours: 24}, false, now)
	if len(got.Items) != 2 || got.Items[0].ID != "go" || got.Items[0].LocalScore != 5 || got.Items[1].LocalScore != 0 {
		t.Fatalf("result = %#v", got)
	}
}

func TestFilterWithFeedbackBlocksItemsAndFeeds(t *testing.T) {
	now := time.Now()
	items := []rss.Item{
		{ID: "blocked-item", FeedName: "A", FeedURL: "https://example.com/a", Title: "One", PublishedAt: now},
		{ID: "blocked-feed", FeedName: "B", FeedURL: "https://example.com/b", Title: "Two", PublishedAt: now},
		{ID: "candidate", FeedName: "C", FeedURL: "https://example.com/c", Title: "Three", PublishedAt: now},
	}
	result := FilterWithFeedback(items, nil, FeedbackRules{
		BlockedItemIDs:  map[string]bool{"blocked-item": true},
		BlockedFeedURLs: map[string]bool{"https://example.com/b": true},
	}, config.Profile{}, config.Settings{LookbackHours: 72}, false, now)
	if result.Stats.FeedbackBlocked != 2 {
		t.Fatalf("feedback blocked = %d, want 2", result.Stats.FeedbackBlocked)
	}
	if len(result.Items) != 1 || result.Items[0].ID != "candidate" {
		t.Fatalf("items = %+v, want only candidate", result.Items)
	}
}

func TestEngineeringPracticeRanksBeforeSkillsAndCodex(t *testing.T) {
	now := time.Now()
	items := []rss.Item{
		{ID: "codex", Title: "Codex usage update", PublishedAt: now},
		{ID: "skills", Title: "A useful MCP skill", PublishedAt: now.Add(-time.Minute)},
		{ID: "practice", Title: "Loop engineering retrospective", PublishedAt: now.Add(-2 * time.Minute)},
	}
	result := Filter(items, nil, config.Profile{}, config.Settings{LookbackHours: 24}, false, now)
	if len(result.Items) != 3 || result.Items[0].ID != "practice" || result.Items[1].ID != "skills" || result.Items[2].ID != "codex" {
		t.Fatalf("order=%+v", result.Items)
	}
}
