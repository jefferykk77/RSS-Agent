package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

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

	response, err = http.Get(server.URL + "/api/digest?profile=missing")
	if err != nil {
		t.Fatalf("GET invalid profile error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET invalid profile status = %s", response.Status)
	}
}
