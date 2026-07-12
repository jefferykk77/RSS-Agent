package rss

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeffery/rss-agent/internal/config"
)

type memoryFeedStateStore struct {
	mu    sync.Mutex
	state FeedFetchState
	ok    bool
}

func (s *memoryFeedStateStore) GetFeedState(_ context.Context, _ string) (FeedFetchState, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, s.ok, nil
}

func (s *memoryFeedStateStore) SaveFeedState(_ context.Context, state FeedFetchState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state, s.ok = state, true
	return nil
}

func TestFetcherRateLimitCooldownAndRecovery(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests == 1 {
			w.Header().Set("Retry-After", "120")
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>Test</title><item><title>Recovered</title><link>https://example.com/recovered</link></item></channel></rss>`))
	}))
	defer server.Close()

	store := &memoryFeedStateStore{}
	fetcher := NewFetcher(time.Second)
	feed := config.Feed{Name: "Reddit", URL: server.URL}

	result := fetcher.Fetch(context.Background(), []config.Feed{feed}, 20, store)
	if len(result.Errs) != 1 || store.state.LastStatus != http.StatusTooManyRequests || store.state.NextRetryAt.IsZero() {
		t.Fatalf("first fetch result=%+v state=%+v", result, store.state)
	}
	result = fetcher.Fetch(context.Background(), []config.Feed{feed}, 20, store)
	if requests != 1 || len(result.Errs) != 1 || !strings.Contains(result.Errs[0].Error(), "限流冷却中") {
		t.Fatalf("cooldown requests=%d errors=%v", requests, result.Errs)
	}

	store.mu.Lock()
	store.state.NextRetryAt = time.Now().Add(-time.Second)
	store.mu.Unlock()
	result = fetcher.Fetch(context.Background(), []config.Feed{feed}, 20, store)
	if requests != 2 || len(result.Errs) != 0 || len(result.Items) != 1 {
		t.Fatalf("recovery requests=%d result=%+v", requests, result)
	}
	if store.state.FailCount != 0 || !store.state.NextRetryAt.IsZero() || store.state.LastError != "" {
		t.Fatalf("recovered state=%+v", store.state)
	}
}

func TestRetryAtFallbackSchedule(t *testing.T) {
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	wants := []time.Duration{15 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour, 2 * time.Hour}
	for index, want := range wants {
		if got := retryAt("", index+1, now).Sub(now); got != want {
			t.Fatalf("failure %d delay=%v want=%v", index+1, got, want)
		}
	}
}
