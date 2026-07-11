package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jeffery/rss-agent/internal/config"
	"github.com/jeffery/rss-agent/internal/rss"
)

func TestRunOnceCompletesGraphWithoutCandidates(t *testing.T) {
	cfg := config.Sample()
	cfg.Feeds = nil
	cfg.Database.Path = filepath.Join(t.TempDir(), "rss-agent.db")

	summary, err := RunOnce(context.Background(), cfg, RunOptions{DryRun: true})
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if summary.Fetched != 0 || summary.Candidate != 0 || summary.Analyzed != 0 || summary.Pushed != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestNewestPerFeedKeepsConfiguredCount(t *testing.T) {
	base := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	var items []rss.Item
	for _, feed := range []string{"a", "b"} {
		for i := 0; i < 12; i++ {
			items = append(items, rss.Item{FeedURL: feed, Title: feed, PublishedAt: base.Add(-time.Duration(i) * time.Minute)})
		}
	}
	selected := newestPerFeed(items, 10)
	if len(selected) != 20 {
		t.Fatalf("selected=%d, want 20", len(selected))
	}
	counts := map[string]int{}
	for _, item := range selected {
		counts[item.FeedURL]++
	}
	if counts["a"] != 10 || counts["b"] != 10 {
		t.Fatalf("counts=%v", counts)
	}
}

func TestIsDigestWindowUsesConfiguredTimezoneAndHour(t *testing.T) {
	now := time.Date(2026, time.July, 11, 0, 30, 0, 0, time.UTC)
	if !isDigestWindow(now, "Asia/Shanghai", []string{"08:00", "20:00"}) {
		t.Fatal("08:30 Asia/Shanghai should be inside the morning digest hour")
	}
	if isDigestWindow(now.Add(time.Hour), "Asia/Shanghai", []string{"08:00", "20:00"}) {
		t.Fatal("09:30 Asia/Shanghai should be collection-only")
	}
}
