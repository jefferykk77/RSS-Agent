package opml

import (
	"encoding/xml"
	"os"
	"sort"
	"strings"

	"github.com/jeffery/rss-agent/internal/config"
)

type Document struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    Head     `xml:"head"`
	Body    Body     `xml:"body"`
}

type Head struct {
	Title string `xml:"title"`
}

type Body struct {
	Outlines []Outline `xml:"outline"`
}

type Outline struct {
	Text     string    `xml:"text,attr,omitempty"`
	Title    string    `xml:"title,attr,omitempty"`
	Type     string    `xml:"type,attr,omitempty"`
	XMLURL   string    `xml:"xmlUrl,attr,omitempty"`
	HTMLURL  string    `xml:"htmlUrl,attr,omitempty"`
	Category string    `xml:"category,attr,omitempty"`
	Outlines []Outline `xml:"outline,omitempty"`
}

func Import(path string) ([]config.Feed, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc Document
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	feeds := flatten(doc.Body.Outlines, nil)
	sort.SliceStable(feeds, func(i, j int) bool {
		return feeds[i].Name < feeds[j].Name
	})
	return feeds, nil
}

func Export(path string, feeds []config.Feed) error {
	doc := Document{
		Version: "2.0",
		Head:    Head{Title: "RSS Agent Subscriptions"},
	}
	for _, feed := range feeds {
		if feed.Disabled {
			continue
		}
		doc.Body.Outlines = append(doc.Body.Outlines, Outline{
			Text:     feed.Name,
			Title:    feed.Name,
			Type:     "rss",
			XMLURL:   feed.URL,
			Category: strings.Join(feed.Tags, ","),
		})
	}
	data, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	data = append([]byte(xml.Header), data...)
	return os.WriteFile(path, data, 0o644)
}

func flatten(outlines []Outline, inheritedTags []string) []config.Feed {
	var feeds []config.Feed
	for _, outline := range outlines {
		tags := append([]string(nil), inheritedTags...)
		if outline.XMLURL == "" && outline.Text != "" {
			tags = append(tags, outline.Text)
		}
		tags = append(tags, splitTags(outline.Category)...)
		if outline.XMLURL != "" {
			name := firstNonEmpty(outline.Title, outline.Text, outline.XMLURL)
			feeds = append(feeds, config.Feed{
				Name: name,
				URL:  outline.XMLURL,
				Tags: dedupeTags(tags),
			})
		}
		feeds = append(feeds, flatten(outline.Outlines, tags)...)
	}
	return feeds
}

func splitTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '/'
	})
	var tags []string
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			tags = append(tags, part)
		}
	}
	return tags
}

func dedupeTags(tags []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
