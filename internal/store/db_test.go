package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jeffery/rss-agent/internal/agent"
	"github.com/jeffery/rss-agent/internal/rss"
)

func TestDBRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "rss-agent.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	feedState := rss.FeedFetchState{
		FeedURL:       "https://example.com/feed.xml",
		ETag:          `"abc"`,
		LastModified:  "Fri, 10 Jul 2026 01:00:00 GMT",
		LastStatus:    200,
		LastFetchedAt: time.Now(),
	}
	if err := db.SaveFeedState(ctx, feedState); err != nil {
		t.Fatalf("SaveFeedState() error = %v", err)
	}
	gotState, ok, err := db.GetFeedState(ctx, feedState.FeedURL)
	if err != nil {
		t.Fatalf("GetFeedState() error = %v", err)
	}
	if !ok || gotState.ETag != feedState.ETag {
		t.Fatalf("feed state = %+v, ok=%v", gotState, ok)
	}

	item := rss.Item{
		FeedName: "Example",
		FeedURL:  feedState.FeedURL,
		Title:    "A useful post",
		Link:     "https://example.com/post",
		Summary:  "Useful summary",
	}
	item.ID = item.StableID()
	if err := db.UpsertItem(ctx, item); err != nil {
		t.Fatalf("UpsertItem() error = %v", err)
	}
	decision := agent.Decision{
		ItemID:     item.StableID(),
		Score:      8,
		ShouldPush: true,
		Title:      "A useful post",
		Summary:    "值得看",
		Why:        "信息密度高",
		KeyPoints:  []string{"A", "B"},
		Tags:       []string{"rss"},
	}
	if err := db.SaveAnalysis(ctx, item, "profile", "test-model", "model-id", decision); err != nil {
		t.Fatalf("SaveAnalysis() error = %v", err)
	}
	cached, ok, err := db.CachedAnalysis(ctx, item, "profile", time.Hour)
	if err != nil {
		t.Fatalf("CachedAnalysis() error = %v", err)
	}
	if !ok || cached.Decision.Score != 8 || !cached.Cached {
		t.Fatalf("cached = %+v, ok=%v", cached, ok)
	}

	if err := db.MarkSeen(ctx, item, true); err != nil {
		t.Fatalf("MarkSeen() error = %v", err)
	}
	seen, err := db.SeenIDs(ctx)
	if err != nil {
		t.Fatalf("SeenIDs() error = %v", err)
	}
	if !seen[item.StableID()] {
		t.Fatal("item should be seen")
	}

	if err := db.RecordCostEvent(ctx, CostEvent{
		Scope:        "llm",
		Provider:     "ark",
		Model:        "deepseek-v3.2",
		InputTokens:  100,
		OutputTokens: 20,
		CostCNY:      0.01,
	}); err != nil {
		t.Fatalf("RecordCostEvent() error = %v", err)
	}
	cost, err := db.CostSince(ctx, "llm", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("CostSince() error = %v", err)
	}
	if cost <= 0 {
		t.Fatalf("cost = %v, want > 0", cost)
	}
	tokens, err := db.TokensSince(ctx, "ark", "deepseek-v3.2", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("TokensSince() error = %v", err)
	}
	if tokens != 120 {
		t.Fatalf("tokens = %d, want 120", tokens)
	}
}

func TestFeedbackRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "rss-agent.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	item := rss.Item{
		FeedName: "Example",
		FeedURL:  "https://example.com/feed.xml",
		Title:    "A useful post",
		Link:     "https://example.com/post",
	}
	item.ID = item.StableID()
	if err := db.UpsertItem(ctx, item); err != nil {
		t.Fatalf("UpsertItem() error = %v", err)
	}
	recent, err := db.RecentItems(ctx, 10)
	if err != nil {
		t.Fatalf("RecentItems() error = %v", err)
	}
	if len(recent) != 1 || recent[0].ID != item.ID {
		t.Fatalf("recent items = %+v", recent)
	}

	if _, err := db.RecordFeedback(ctx, item.ID, FeedbackLike); err != nil {
		t.Fatalf("RecordFeedback(like) error = %v", err)
	}
	if _, err := db.RecordFeedback(ctx, item.ID, FeedbackDislike); err != nil {
		t.Fatalf("RecordFeedback(dislike) error = %v", err)
	}
	likes, err := db.ListFeedback(ctx, FeedbackLike)
	if err != nil {
		t.Fatalf("ListFeedback(like) error = %v", err)
	}
	if len(likes) != 0 {
		t.Fatalf("likes = %+v, want none after dislike", likes)
	}

	if _, err := db.RecordFeedback(ctx, item.ID, FeedbackBlockFeed); err != nil {
		t.Fatalf("RecordFeedback(block-feed) error = %v", err)
	}
	filters, err := db.FeedbackFilters(ctx)
	if err != nil {
		t.Fatalf("FeedbackFilters() error = %v", err)
	}
	if !filters.BlockedItemIDs[item.ID] {
		t.Fatal("disliked item should be blocked")
	}
	if !filters.BlockedFeedURLs[item.FeedURL] {
		t.Fatal("blocked feed should be present")
	}

	removed, err := db.RemoveFeedback(ctx, item.ID, FeedbackBlockFeed)
	if err != nil {
		t.Fatalf("RemoveFeedback() error = %v", err)
	}
	if !removed {
		t.Fatal("block-feed feedback should be removed")
	}
	filters, err = db.FeedbackFilters(ctx)
	if err != nil {
		t.Fatalf("FeedbackFilters() error = %v", err)
	}
	if filters.BlockedFeedURLs[item.FeedURL] {
		t.Fatal("removed block-feed should no longer block the feed")
	}
}
