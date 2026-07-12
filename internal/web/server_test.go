package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jeffery/rss-agent/internal/agent"
	"github.com/jeffery/rss-agent/internal/config"
	"github.com/jeffery/rss-agent/internal/rss"
	"github.com/jeffery/rss-agent/internal/store"
)

func TestDigestAndFeedbackAPI(t *testing.T) {
	ctx := context.Background()
	cfg := config.Sample()
	db, err := store.Open(filepath.Join(t.TempDir(), "rss-agent.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	item := rss.Item{
		FeedName: "Example Feed",
		FeedURL:  "https://example.com/feed.xml",
		Title:    "A useful post",
		Link:     "https://example.com/post",
		Summary:  "Source summary",
		Content:  "Full article body",
	}
	item.ID = item.StableID()
	if err := db.UpsertItem(ctx, item); err != nil {
		t.Fatalf("UpsertItem() error = %v", err)
	}
	if err := db.UpsertProfileItem(ctx, config.DefaultProfileName, item); err != nil {
		t.Fatalf("UpsertProfileItem() error = %v", err)
	}
	decision := agent.Decision{
		ItemID:     item.ID,
		Score:      9,
		ShouldPush: true,
		Title:      "Analyzed title",
		Summary:    "Analyzed summary",
		Why:        "Matches the configured interests",
		KeyPoints:  []string{"Useful point"},
		Tags:       []string{"go"},
	}
	if err := db.SaveAnalysis(ctx, item, cfg.ProfileHash(), "test-model", "model-id", decision); err != nil {
		t.Fatalf("SaveAnalysis() error = %v", err)
	}
	if err := db.MarkSeenForProfile(ctx, config.DefaultProfileName, item, true); err != nil {
		t.Fatalf("MarkSeenForProfile() error = %v", err)
	}

	server := httptest.NewServer(New(cfg, db).Handler())
	defer server.Close()

	response, err := http.Get(server.URL + "/api/digest?profile=default")
	if err != nil {
		t.Fatalf("GET digest error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET digest status = %s", response.Status)
	}
	var digest digestResponse
	if err := json.NewDecoder(response.Body).Decode(&digest); err != nil {
		t.Fatalf("decode digest: %v", err)
	}
	if len(digest.Items) != 1 || digest.Items[0].Title != "A useful post" || digest.Items[0].Score != 9 || !digest.Items[0].Pushed {
		t.Fatalf("digest = %+v", digest)
	}

	body := bytes.NewBufferString(`{"profile":"default","item_id":"` + item.ID + `","action":"save"}`)
	response, err = http.Post(server.URL+"/api/feedback", "application/json", body)
	if err != nil {
		t.Fatalf("POST feedback error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST feedback status = %s", response.Status)
	}

	request, err := http.NewRequest(http.MethodDelete, server.URL+"/api/feedback?profile=default&item_id="+item.ID+"&action=save", nil)
	if err != nil {
		t.Fatalf("new DELETE request: %v", err)
	}
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("DELETE feedback error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("DELETE feedback status = %s", response.Status)
	}
	var removed feedbackResponse
	if err := json.NewDecoder(response.Body).Decode(&removed); err != nil {
		t.Fatalf("decode delete feedback: %v", err)
	}
	if !removed.Removed {
		t.Fatalf("delete response = %+v, want removed", removed)
	}
}

func TestStaticUIAndInvalidProfile(t *testing.T) {
	cfg := config.Sample()
	db, err := store.Open(filepath.Join(t.TempDir(), "rss-agent.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	server := httptest.NewServer(New(cfg, db).Handler())
	defer server.Close()

	response, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("GET UI error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET UI status = %s", response.Status)
	}
	page, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read UI: %v", err)
	}
	if !strings.Contains(string(page), "RSS Agent") {
		t.Fatal("UI does not contain RSS Agent title")
	}
	if !strings.Contains(string(page), "/assets/") {
		t.Fatal("UI does not reference the Vite asset bundle")
	}
	assetStart := strings.Index(string(page), "/assets/")
	if assetStart < 0 {
		t.Fatal("unable to locate a Vite asset URL")
	}
	assetEnd := strings.Index(string(page)[assetStart:], "\"")
	if assetEnd < 0 {
		t.Fatal("unable to locate the end of a Vite asset URL")
	}
	assetURL := string(page)[assetStart : assetStart+assetEnd]
	assetResponse, err := http.Get(server.URL + assetURL)
	if err != nil {
		t.Fatalf("GET UI asset error = %v", err)
	}
	defer assetResponse.Body.Close()
	if assetResponse.StatusCode != http.StatusOK || !strings.Contains(assetResponse.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("GET UI asset status=%s cache=%q", assetResponse.Status, assetResponse.Header.Get("Cache-Control"))
	}

	response, err = http.Get(server.URL + "/api/digest?profile=missing")
	if err != nil {
		t.Fatalf("GET invalid profile error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET invalid profile status = %s", response.Status)
	}

	response, err = http.Post(server.URL+"/api/analyze?profile=default", "application/json", nil)
	if err != nil {
		t.Fatalf("POST analyze error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST analyze without item_id status = %s", response.Status)
	}
}

func TestMakeDigestItemUsesEmptyJSONArrays(t *testing.T) {
	digest := makeDigestItem(store.DigestItem{})
	if digest.KeyPoints == nil || digest.Tags == nil || digest.Feedback == nil {
		t.Fatalf("digest arrays must not be nil: %+v", digest)
	}
}

func TestDigestCursorAndPendingAnalysisStatus(t *testing.T) {
	ctx := context.Background()
	cfg := config.Sample()
	db, err := store.Open(filepath.Join(t.TempDir(), "rss-agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i := 0; i < 12; i++ {
		item := rss.Item{FeedName: "Feed", FeedURL: "feed", Title: fmt.Sprintf("Item %02d", i), Link: fmt.Sprintf("https://example.com/%d", i), PublishedAt: time.Now().Add(-time.Duration(i) * time.Minute)}
		if err := db.UpsertItem(ctx, item); err != nil {
			t.Fatal(err)
		}
		if err := db.UpsertProfileItem(ctx, "default", item); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(New(cfg, db).Handler())
	defer server.Close()
	response, err := http.Get(server.URL + "/api/digest?profile=default&limit=5&order=newest")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var page digestResponse
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 5 || page.NextCursor != "5" || page.Items[0].AnalysisStatus != "pending" {
		t.Fatalf("page=%+v", page)
	}
}

func TestCurrentRunAndDigestUpdates(t *testing.T) {
	ctx := context.Background()
	cfg := config.Sample()
	db, err := store.Open(filepath.Join(t.TempDir(), "rss-agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	item := rss.Item{ID: "updated", FeedName: "Feed", FeedURL: "feed", Title: "Updated", Content: "body"}
	if err := db.UpsertItem(ctx, item); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertProfileItem(ctx, "default", item); err != nil {
		t.Fatal(err)
	}
	decision := agent.Decision{ItemID: item.ID, Score: 9, ShouldPush: true, Title: "Analyzed", Summary: "Summary", Why: "Why", KeyPoints: []string{"Point"}, Tags: []string{"tag"}}
	if err := db.SaveAnalysis(ctx, item, cfg.ProfileHash(), "model", "id", decision); err != nil {
		t.Fatal(err)
	}
	runID, err := db.CreateAnalysisRun(ctx, "default", "completed")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetAnalysisRunTotals(ctx, runID, 1, 1, "completed"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(cfg, db).Handler())
	defer server.Close()
	response, err := http.Get(server.URL + "/api/digest/updates?profile=default&item_id=updated")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var updates digestResponse
	if err := json.NewDecoder(response.Body).Decode(&updates); err != nil {
		t.Fatal(err)
	}
	if len(updates.Items) != 1 || updates.Items[0].AnalysisStatus != "completed" || updates.Items[0].Score != 9 {
		t.Fatalf("updates=%+v", updates)
	}
	response, err = http.Get(server.URL + "/api/analysis-runs/current?profile=default")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var run store.AnalysisRun
	if err := json.NewDecoder(response.Body).Decode(&run); err != nil {
		t.Fatal(err)
	}
	if run.ID != runID || run.Analyzed != 1 {
		t.Fatalf("run=%+v", run)
	}
}

func TestIngestAndPreferenceFeedbackAPI(t *testing.T) {
	cfg := config.Sample()
	db, err := store.Open(filepath.Join(t.TempDir(), "rss-agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	server := httptest.NewServer(New(cfg, db).Handler())
	defer server.Close()

	body := bytes.NewBufferString(`{"profile":"default","url":"https://example.com/loop","title":"A month of loop engineering","content":"A detailed implementation retrospective.","tags":["paradigm"]}`)
	response, err := http.Post(server.URL+"/api/ingest", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("ingest status = %s", response.Status)
	}
	var created map[string]string
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	feedbackBody := bytes.NewBufferString(`{"profile":"default","item_id":"` + created["item_id"] + `","action":"more-like-this"}`)
	feedbackResponse, err := http.Post(server.URL+"/api/feedback", "application/json", feedbackBody)
	if err != nil {
		t.Fatal(err)
	}
	defer feedbackResponse.Body.Close()
	if feedbackResponse.StatusCode != http.StatusOK {
		t.Fatalf("feedback status = %s", feedbackResponse.Status)
	}

	digestHTTP, err := http.Get(server.URL + "/api/digest?profile=default")
	if err != nil {
		t.Fatal(err)
	}
	defer digestHTTP.Body.Close()
	var digest digestResponse
	if err := json.NewDecoder(digestHTTP.Body).Decode(&digest); err != nil {
		t.Fatal(err)
	}
	if len(digest.Items) != 1 || !containsString(digest.Items[0].Feedback, "more-like-this") {
		t.Fatalf("digest feedback = %+v", digest.Items)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestSourceHealthState(t *testing.T) {
	cases := []struct {
		name  string
		state store.SourceHealth
		want  string
	}{
		{"healthy", store.SourceHealth{Status: http.StatusOK}, "healthy"},
		{"rate limited", store.SourceHealth{Status: http.StatusTooManyRequests, FailCount: 1, NextRetryAt: time.Now().Add(time.Minute)}, "rate_limited"},
		{"expired rate limit", store.SourceHealth{Status: http.StatusTooManyRequests, FailCount: 1, NextRetryAt: time.Now().Add(-time.Minute)}, "error"},
		{"unknown", store.SourceHealth{}, "unknown"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := sourceHealthState(test.state); got != test.want {
				t.Fatalf("sourceHealthState()=%q want=%q", got, test.want)
			}
		})
	}
}
