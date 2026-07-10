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
