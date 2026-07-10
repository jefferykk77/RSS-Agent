package store

import (
	"path/filepath"
	"testing"

	"github.com/jeffery/rss-agent/internal/rss"
)

func TestStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	state, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	item := rss.Item{Title: "Hello", Link: "https://example.com/hello", FeedName: "Example"}
	state.Mark(item, true)
	if !state.IsSeen(item.StableID()) {
		t.Fatal("item should be seen after mark")
	}
	if err := Save(path, state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload error = %v", err)
	}
	if !reloaded.IsSeen(item.StableID()) {
		t.Fatal("item should survive reload")
	}
}
