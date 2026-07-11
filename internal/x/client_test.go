package x

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeffery/rss-agent/internal/config"
)

func TestFetchSearchMapsPostsToItems(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/2/tweets/search/recent" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.URL.Query().Get("query"); got != "AI lang:en -is:retweet" {
			t.Fatalf("query = %q", got)
		}
		if got := r.URL.Query().Get("max_results"); got != "20" {
			t.Fatalf("max_results = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "data": [{
    "id": "42",
    "text": "A concise AI update #agents",
    "author_id": "7",
    "created_at": "2026-07-10T10:00:00Z",
    "entities": {"hashtags": [{"tag": "agents"}]}
  }],
  "includes": {"users": [{"id": "7", "name": "Ada", "username": "ada"}]}
}`))
	}))
	defer server.Close()

	client := New(server.URL+"/2", "token", server.Client())
	items, err := client.FetchSearch(context.Background(), config.XSearch{
		Name:       "X AI",
		Query:      "AI lang:en -is:retweet",
		Tags:       []string{"ai", "x"},
		MaxResults: 20,
	})
	if err != nil {
		t.Fatalf("FetchSearch() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	item := items[0]
	if item.ID != "x:42" || item.Link != "https://x.com/ada/status/42" || item.Author != "Ada" {
		t.Fatalf("item = %+v", item)
	}
	if len(item.Categories) != 1 || item.Categories[0] != "agents" {
		t.Fatalf("categories = %+v", item.Categories)
	}
}

func TestFetchSearchesReportsMissingBearer(t *testing.T) {
	result := New("", "", nil).FetchSearches(context.Background(), []config.XSearch{{Name: "X AI", Query: "AI", MaxResults: 20}})
	if len(result.Items) != 0 || len(result.Errs) != 1 {
		t.Fatalf("result = %+v", result)
	}
}
