package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/jeffery/rss-agent/internal/rss"
)

type State struct {
	Seen map[string]SeenItem `json:"seen"`
}

type SeenItem struct {
	Title    string    `json:"title"`
	Link     string    `json:"link"`
	FeedName string    `json:"feed_name"`
	SeenAt   time.Time `json:"seen_at"`
	Pushed   bool      `json:"pushed"`
}

func Load(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &State{Seen: map[string]SeenItem{}}, nil
		}
		return nil, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	if state.Seen == nil {
		state.Seen = map[string]SeenItem{}
	}
	return &state, nil
}

func Save(path string, state *State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (s *State) IsSeen(id string) bool {
	_, ok := s.Seen[id]
	return ok
}

func (s *State) Mark(item rss.Item, pushed bool) {
	if s.Seen == nil {
		s.Seen = map[string]SeenItem{}
	}
	s.Seen[item.StableID()] = SeenItem{
		Title:    item.Title,
		Link:     item.Link,
		FeedName: item.FeedName,
		SeenAt:   time.Now(),
		Pushed:   pushed,
	}
}
