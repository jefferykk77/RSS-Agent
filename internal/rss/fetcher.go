package rss

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jeffery/rss-agent/internal/config"
	"github.com/mmcdole/gofeed"
)

type Fetcher struct {
	client    *http.Client
	userAgent string
}

type FeedFetchState struct {
	FeedURL       string
	ETag          string
	LastModified  string
	LastStatus    int
	LastError     string
	FailCount     int
	LastFetchedAt time.Time
	NextRetryAt   time.Time
}

type FeedStateStore interface {
	GetFeedState(ctx context.Context, feedURL string) (FeedFetchState, bool, error)
	SaveFeedState(ctx context.Context, state FeedFetchState) error
}

type FetchResult struct {
	Items       []Item
	NotModified int
	Errs        []error
}

func NewFetcher(timeout time.Duration) *Fetcher {
	client := &http.Client{Timeout: timeout}
	return &Fetcher{
		client:    client,
		userAgent: "rss-agent/0.1 (+https://github.com/jeffery/rss-agent)",
	}
}

func (f *Fetcher) Fetch(ctx context.Context, feeds []config.Feed, maxItemsPerFeed int, stores ...FeedStateStore) FetchResult {
	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		result FetchResult
		store  FeedStateStore
	)
	if len(stores) > 0 {
		store = stores[0]
	}

	for _, feed := range feeds {
		feed := feed
		wg.Add(1)
		go func() {
			defer wg.Done()
			parsed, notModified, err := f.fetchOne(ctx, feed, store)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				result.Errs = append(result.Errs, fmt.Errorf("%s: %w", feed.Name, err))
				return
			}
			if notModified {
				result.NotModified++
				return
			}

			count := 0
			for _, src := range parsed.Items {
				if maxItemsPerFeed > 0 && count >= maxItemsPerFeed {
					break
				}
				item := Item{
					FeedName:    feed.Name,
					FeedURL:     feed.URL,
					FeedTags:    append([]string(nil), feed.Tags...),
					Title:       src.Title,
					Link:        src.Link,
					GUID:        src.GUID,
					Categories:  append([]string(nil), src.Categories...),
					PublishedAt: timeFromPtr(src.PublishedParsed),
					UpdatedAt:   timeFromPtr(src.UpdatedParsed),
					Summary:     src.Description,
					Content:     src.Content,
				}
				if len(src.Authors) > 0 {
					item.Author = src.Authors[0].Name
				}
				item.ID = item.StableID()
				result.Items = append(result.Items, item)
				count++
			}
		}()
	}

	wg.Wait()
	sort.Slice(result.Items, func(i, j int) bool {
		return result.Items[i].Time().After(result.Items[j].Time())
	})
	return result
}

func (f *Fetcher) fetchOne(ctx context.Context, feed config.Feed, store FeedStateStore) (*gofeed.Feed, bool, error) {
	state := FeedFetchState{FeedURL: feed.URL}
	if store != nil {
		saved, ok, err := store.GetFeedState(ctx, feed.URL)
		if err != nil {
			return nil, false, err
		}
		if ok {
			state = saved
		}
	}
	if state.NextRetryAt.After(time.Now()) {
		return nil, false, fmt.Errorf("限流冷却中，%s 后重试", state.NextRetryAt.Local().Format("2006-01-02 15:04:05"))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.URL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", f.userAgent)
	if state.ETag != "" {
		req.Header.Set("If-None-Match", state.ETag)
	}
	if state.LastModified != "" {
		req.Header.Set("If-Modified-Since", state.LastModified)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		state.LastStatus = 0
		state.LastError = err.Error()
		state.FailCount++
		state.LastFetchedAt = time.Now()
		saveFeedState(ctx, store, state)
		return nil, false, err
	}
	defer resp.Body.Close()

	state.LastStatus = resp.StatusCode
	state.LastFetchedAt = time.Now()
	if resp.StatusCode == http.StatusNotModified {
		state.LastError = ""
		state.FailCount = 0
		state.NextRetryAt = time.Time{}
		saveFeedState(ctx, store, state)
		return nil, true, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		state.LastError = resp.Status
		state.FailCount++
		if resp.StatusCode == http.StatusTooManyRequests {
			state.NextRetryAt = retryAt(resp.Header.Get("Retry-After"), state.FailCount, time.Now())
		} else {
			state.NextRetryAt = time.Time{}
		}
		saveFeedState(ctx, store, state)
		return nil, false, fmt.Errorf("HTTP %s", resp.Status)
	}

	parser := gofeed.NewParser()
	parsed, err := parser.Parse(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		state.LastError = err.Error()
		state.FailCount++
		saveFeedState(ctx, store, state)
		return nil, false, err
	}
	state.ETag = resp.Header.Get("ETag")
	state.LastModified = resp.Header.Get("Last-Modified")
	state.LastError = ""
	state.FailCount = 0
	state.NextRetryAt = time.Time{}
	saveFeedState(ctx, store, state)
	return parsed, false, nil
}

func retryAt(raw string, failCount int, now time.Time) time.Time {
	raw = strings.TrimSpace(raw)
	if seconds, err := strconv.Atoi(raw); err == nil && seconds >= 0 {
		return now.Add(time.Duration(seconds) * time.Second)
	}
	if parsed, err := http.ParseTime(raw); err == nil && parsed.After(now) {
		return parsed
	}
	delays := []time.Duration{15 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour}
	index := failCount - 1
	if index < 0 {
		index = 0
	}
	if index >= len(delays) {
		index = len(delays) - 1
	}
	return now.Add(delays[index])
}

func saveFeedState(ctx context.Context, store FeedStateStore, state FeedFetchState) {
	if store != nil {
		_ = store.SaveFeedState(ctx, state)
	}
}

func timeFromPtr(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
