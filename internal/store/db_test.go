package store

import (
	"context"
	"fmt"
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
		NextRetryAt:   time.Now().Add(15 * time.Minute),
	}
	if err := db.SaveFeedState(ctx, feedState); err != nil {
		t.Fatalf("SaveFeedState() error = %v", err)
	}
	gotState, ok, err := db.GetFeedState(ctx, feedState.FeedURL)
	if err != nil {
		t.Fatalf("GetFeedState() error = %v", err)
	}
	if !ok || gotState.ETag != feedState.ETag || gotState.NextRetryAt.IsZero() {
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

func TestAnalysisQueueDeduplicatesPromotesAndCompletes(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	item := rss.Item{FeedName: "Feed", FeedURL: "feed", Title: "Queued", Link: "https://example.com/q", Content: "body"}
	if err := db.UpsertItem(ctx, item); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertProfileItem(ctx, "default", item); err != nil {
		t.Fatal(err)
	}
	if _, err := db.EnqueueAnalysis(ctx, 1, "default", "profile", "v1", item, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := db.EnqueueAnalysis(ctx, 1, "default", "profile", "v1", item, 80); err != nil {
		t.Fatal(err)
	}
	tasks, err := db.ClaimAnalysisTasks(ctx, "default", 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Priority != 80 {
		t.Fatalf("tasks=%+v", tasks)
	}
	if err := db.CompleteAnalysisTasks(ctx, []int64{tasks[0].ID}); err != nil {
		t.Fatal(err)
	}
	stats, err := db.AnalysisQueueStats(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Completed != 1 || stats.Pending != 0 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestRecoveryRunIncludesExistingCompletedAndPendingTasks(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "recovery.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i := 0; i < 2; i++ {
		item := rss.Item{ID: fmt.Sprintf("item-%d", i), FeedName: "Feed", FeedURL: "feed", Title: "Item", Content: fmt.Sprint(i)}
		if err := db.UpsertItem(ctx, item); err != nil {
			t.Fatal(err)
		}
		if err := db.UpsertProfileItem(ctx, "default", item); err != nil {
			t.Fatal(err)
		}
		if _, err := db.EnqueueAnalysis(ctx, 0, "default", "profile", "v1", item, 10); err != nil {
			t.Fatal(err)
		}
	}
	tasks, err := db.ClaimAnalysisTasks(ctx, "default", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CompleteAnalysisTasks(ctx, []int64{tasks[0].ID}); err != nil {
		t.Fatal(err)
	}
	runID, err := db.EnsureRecoveryRun(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	if runID == 0 {
		t.Fatal("missing recovery run")
	}
	run, ok, err := db.CurrentAnalysisRun(ctx, "default")
	if err != nil || !ok {
		t.Fatalf("run=%+v ok=%v err=%v", run, ok, err)
	}
	if run.Total != 2 || run.Analyzed != 1 || run.Pending != 1 {
		t.Fatalf("run=%+v", run)
	}
}

func TestCleanupOldItemsProtectsSavedLaterManualAndDigest(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "cleanup.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := time.Now().Add(-72 * time.Hour)
	items := []rss.Item{{ID: "delete", FeedName: "F", FeedURL: "feed", Title: "D", PublishedAt: old}, {ID: "save", FeedName: "F", FeedURL: "feed", Title: "S", PublishedAt: old}, {ID: "later", FeedName: "F", FeedURL: "feed", Title: "L", PublishedAt: old}, {ID: "manual", FeedName: "M", FeedURL: "manual://default", Title: "M", PublishedAt: old}, {ID: "digest", FeedName: "F", FeedURL: "feed", Title: "G", PublishedAt: old}}
	if err := db.UpsertItemsForProfile(ctx, "default", items); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RecordFeedbackForProfile(ctx, "default", "save", FeedbackSave); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RecordFeedbackForProfile(ctx, "default", "later", FeedbackLater); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordDigestEdition(ctx, "default", "manual", []string{"digest"}, true); err != nil {
		t.Fatal(err)
	}
	deleted, err := db.CleanupOldItems(ctx, "default", time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted=%d", deleted)
	}
	var count int
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 4 {
		t.Fatalf("remaining=%d", count)
	}
}

func TestDigestPageFiltersSourceAndUsesHybridOrder(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "page.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now()
	items := []rss.Item{{ID: "new", FeedName: "A", FeedURL: "a", Title: "New", PublishedAt: now}, {ID: "low", FeedName: "A", FeedURL: "a", Title: "Low", PublishedAt: now.Add(-time.Hour)}, {ID: "high", FeedName: "A", FeedURL: "a", Title: "High", PublishedAt: now.Add(-2 * time.Hour)}, {ID: "other", FeedName: "B", FeedURL: "b", Title: "Other", PublishedAt: now}}
	if err := db.UpsertItemsForProfile(ctx, "default", items); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct {
		id    string
		score int
	}{{"low", 2}, {"high", 9}} {
		item, _ := db.ItemForProfile(ctx, "default", entry.id)
		decision := agent.Decision{ItemID: entry.id, Score: entry.score, Title: "T", Summary: "S", Why: "W", KeyPoints: []string{"P"}, Tags: []string{"t"}}
		if err := db.SaveAnalysis(ctx, item, "profile", "m", "m", decision); err != nil {
			t.Fatal(err)
		}
	}
	page, err := db.DigestPageForProfile(ctx, "default", "profile", "a", "hybrid", 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Items) != 3 || page.Items[0].ID != "high" || page.Items[1].ID != "low" || page.Items[2].ID != "new" {
		t.Fatalf("page=%+v", page)
	}
}

func TestDigestPageRecommendedUsesModelRecommendation(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "recommended.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now()
	items := []rss.Item{
		{ID: "high-not-recommended", FeedName: "A", FeedURL: "a", Title: "High", PublishedAt: now},
		{ID: "recommended", FeedName: "A", FeedURL: "a", Title: "Recommended", PublishedAt: now.Add(-time.Hour)},
	}
	if err := db.UpsertItemsForProfile(ctx, "default", items); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct {
		id          string
		score       int
		recommended bool
	}{{"high-not-recommended", 10, false}, {"recommended", 7, true}} {
		item, _ := db.ItemForProfile(ctx, "default", entry.id)
		decision := agent.Decision{ItemID: entry.id, Score: entry.score, ShouldPush: entry.recommended, Title: "T", Summary: "S", Why: "W", KeyPoints: []string{"P"}, Tags: []string{"t"}}
		if err := db.SaveAnalysis(ctx, item, "profile", "m", "m", decision); err != nil {
			t.Fatal(err)
		}
	}
	page, err := db.DigestPageForProfile(ctx, "default", "profile", "", "recommended", 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != "recommended" {
		t.Fatalf("recommended page=%+v", page)
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

func TestProfileScopedItemsSeenAndFeedback(t *testing.T) {
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
		Summary:  "Useful source summary",
		Content:  "Full article body",
	}
	item.ID = item.StableID()
	if err := db.UpsertItem(ctx, item); err != nil {
		t.Fatalf("UpsertItem() error = %v", err)
	}
	for _, profileID := range []string{"default", "product"} {
		if err := db.UpsertProfileItem(ctx, profileID, item); err != nil {
			t.Fatalf("UpsertProfileItem(%s) error = %v", profileID, err)
		}
	}

	if err := db.MarkSeenForProfile(ctx, "default", item, true); err != nil {
		t.Fatalf("MarkSeenForProfile() error = %v", err)
	}
	defaultSeen, err := db.SeenIDsForProfile(ctx, "default")
	if err != nil {
		t.Fatalf("SeenIDsForProfile(default) error = %v", err)
	}
	productSeen, err := db.SeenIDsForProfile(ctx, "product")
	if err != nil {
		t.Fatalf("SeenIDsForProfile(product) error = %v", err)
	}
	if !defaultSeen[item.ID] || productSeen[item.ID] {
		t.Fatalf("seen isolation failed: default=%v product=%v", defaultSeen, productSeen)
	}

	if _, err := db.RecordFeedbackForProfile(ctx, "product", item.ID, FeedbackBlockFeed); err != nil {
		t.Fatalf("RecordFeedbackForProfile() error = %v", err)
	}
	defaultFilters, err := db.FeedbackFiltersForProfile(ctx, "default")
	if err != nil {
		t.Fatalf("FeedbackFiltersForProfile(default) error = %v", err)
	}
	productFilters, err := db.FeedbackFiltersForProfile(ctx, "product")
	if err != nil {
		t.Fatalf("FeedbackFiltersForProfile(product) error = %v", err)
	}
	if defaultFilters.BlockedFeedURLs[item.FeedURL] || !productFilters.BlockedFeedURLs[item.FeedURL] {
		t.Fatalf("feedback isolation failed: default=%v product=%v", defaultFilters, productFilters)
	}

	defaultItems, err := db.RecentItemsForProfile(ctx, "default", 10)
	if err != nil {
		t.Fatalf("RecentItemsForProfile(default) error = %v", err)
	}
	productItems, err := db.RecentItemsForProfile(ctx, "product", 10)
	if err != nil {
		t.Fatalf("RecentItemsForProfile(product) error = %v", err)
	}
	if len(defaultItems) != 1 || len(productItems) != 1 {
		t.Fatalf("profile items = default:%+v product:%+v", defaultItems, productItems)
	}

	decision := agent.Decision{
		ItemID:     item.ID,
		Score:      9,
		ShouldPush: true,
		Title:      "Analyzed title",
		Summary:    "Analyzed summary",
		Why:        "Relevant to the product profile",
		KeyPoints:  []string{"One useful point"},
		Tags:       []string{"product"},
	}
	if err := db.SaveAnalysis(ctx, item, "product-profile", "test-model", "model-id", decision); err != nil {
		t.Fatalf("SaveAnalysis() error = %v", err)
	}
	digest, err := db.DigestItemsForProfile(ctx, "product", "product-profile", 10)
	if err != nil {
		t.Fatalf("DigestItemsForProfile() error = %v", err)
	}
	if len(digest) != 1 {
		t.Fatalf("digest = %+v, want one item", digest)
	}
	if digest[0].Score != 9 || digest[0].Summary != "Analyzed summary" || digest[0].Content != "Full article body" {
		t.Fatalf("digest item = %+v", digest[0])
	}
	if len(digest[0].Feedback) != 1 || digest[0].Feedback[0] != FeedbackBlockFeed {
		t.Fatalf("digest feedback = %+v", digest[0].Feedback)
	}
}

func TestItemForProfileRestoresStoredRSSItem(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "rss-agent.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	item := rss.Item{
		FeedName:   "Example",
		FeedURL:    "https://example.com/feed.xml",
		FeedTags:   []string{"ai"},
		Title:      "Stored item",
		Link:       "https://example.com/post",
		GUID:       "example-guid",
		Author:     "Author",
		Categories: []string{"agents"},
		Summary:    "Summary",
		Content:    "Full text",
	}
	item.ID = item.StableID()
	if err := db.UpsertItem(ctx, item); err != nil {
		t.Fatalf("UpsertItem() error = %v", err)
	}
	if err := db.UpsertProfileItem(ctx, "default", item); err != nil {
		t.Fatalf("UpsertProfileItem() error = %v", err)
	}

	got, err := db.ItemForProfile(ctx, "default", item.ID)
	if err != nil {
		t.Fatalf("ItemForProfile() error = %v", err)
	}
	if got.ID != item.ID || got.Content != item.Content || got.Author != item.Author {
		t.Fatalf("item = %+v", got)
	}
	if len(got.FeedTags) != 1 || got.FeedTags[0] != "ai" || len(got.Categories) != 1 || got.Categories[0] != "agents" {
		t.Fatalf("stored tags = %+v categories = %+v", got.FeedTags, got.Categories)
	}
}
