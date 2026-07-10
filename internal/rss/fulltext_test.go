package rss

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestEnrichFullTextReplacesShortRSSContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>Ignore me</title></head><body><nav>Navigation</nav><article><h1>Article title</h1><p>This is the detailed article body with implementation details.</p><p>It is long enough to replace the RSS summary.</p></article><script>ignore()</script></body></html>`))
	}))
	defer server.Close()

	result := NewFetcher(time.Second).EnrichFullText(context.Background(), []Item{{
		ID:      "item-1",
		Link:    server.URL,
		Summary: "Short summary",
	}}, 80, 500)
	if len(result.Errs) != 0 {
		t.Fatalf("EnrichFullText() errors = %v", result.Errs)
	}
	if len(result.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(result.Items))
	}
	content := result.Items[0].Content
	if !strings.Contains(content, "detailed article body") {
		t.Fatalf("content missing article text: %q", content)
	}
	if strings.Contains(content, "Navigation") || strings.Contains(content, "ignore()") {
		t.Fatalf("content contains ignored markup text: %q", content)
	}
	if !strings.Contains(result.Items[0].Snippet(500), "detailed article body") {
		t.Fatalf("snippet did not prefer extracted content: %q", result.Items[0].Snippet(500))
	}
}

func TestEnrichFullTextSkipsSufficientRSSContent(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<p>unused</p>"))
	}))
	defer server.Close()

	content := strings.Repeat("already complete RSS content ", 10)
	result := NewFetcher(time.Second).EnrichFullText(context.Background(), []Item{{
		ID:      "item-1",
		Link:    server.URL,
		Content: content,
	}}, 100, 500)
	if len(result.Errs) != 0 {
		t.Fatalf("EnrichFullText() errors = %v", result.Errs)
	}
	if calls.Load() != 0 {
		t.Fatalf("article requests = %d, want 0", calls.Load())
	}
	if result.Items[0].Content != content {
		t.Fatalf("content was changed: %q", result.Items[0].Content)
	}
}
