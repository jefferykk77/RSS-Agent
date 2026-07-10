package rss

import (
	"crypto/sha256"
	"encoding/hex"
	"html"
	"regexp"
	"strings"
	"time"
)

var htmlTagPattern = regexp.MustCompile(`<[^>]+>`)

type Item struct {
	ID          string
	FeedName    string
	FeedURL     string
	FeedTags    []string
	Title       string
	Link        string
	GUID        string
	Author      string
	Categories  []string
	PublishedAt time.Time
	UpdatedAt   time.Time
	Summary     string
	Content     string
}

func (i Item) StableID() string {
	if i.ID != "" {
		return i.ID
	}
	raw := firstNonEmpty(i.GUID, i.Link, i.FeedName+"|"+i.Title+"|"+i.PublishedAt.Format(time.RFC3339))
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])[:16]
}

func (i Item) ContentHash() string {
	raw := strings.Join([]string{
		i.Title,
		i.Link,
		i.Summary,
		i.Content,
		strings.Join(i.Categories, "|"),
	}, "\n")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])[:16]
}

func (i Item) Time() time.Time {
	if !i.PublishedAt.IsZero() {
		return i.PublishedAt
	}
	return i.UpdatedAt
}

func (i Item) Snippet(limit int) string {
	text := firstNonEmpty(i.Summary, i.Content)
	text = cleanText(text)
	if limit <= 0 || len([]rune(text)) <= limit {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:limit])) + "..."
}

func cleanText(s string) string {
	s = htmlTagPattern.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
