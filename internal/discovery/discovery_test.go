package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchHeadingPageCreatesIndependentItems(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html><body><h2>July 2026 update</h2><p>Codex added a useful workflow.</p><h2>June 2026 update</h2><p>Earlier details.</p></body></html>`))
	}))
	defer server.Close()
	items, err := fetchHeadingPage(context.Background(), server.Client(), "Release Notes", server.URL, []string{"codex"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Title != "July 2026 update" || items[0].Content == "" {
		t.Fatalf("items=%+v", items)
	}
}
