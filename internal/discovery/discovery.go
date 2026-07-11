package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/jeffery/rss-agent/internal/rss"
)

type Result struct {
	Items  []rss.Item
	Errors []error
}

type Source struct {
	Name, URL string
	Tags      []string
}

func Sources() []Source {
	return []Source{
		{Name: "Anthropic News", URL: "https://www.anthropic.com/news", Tags: []string{"primary", "anthropic", "claude"}},
		{Name: "ChatGPT Release Notes", URL: "https://help.openai.com/en/articles/6825453-chatgpt-release-notes", Tags: []string{"primary", "openai", "codex"}},
		{Name: "Model Release Notes", URL: "https://help.openai.com/en/articles/9624314-model-release-notes", Tags: []string{"primary", "openai", "model-infra", "codex"}},
		{Name: "Claude Code Changelog", URL: "https://raw.githubusercontent.com/anthropics/claude-code/main/CHANGELOG.md", Tags: []string{"primary", "claude-code", "codex-transfer"}},
		{Name: "Bluesky · AI Builders", URL: "https://bsky.app", Tags: []string{"community", "applied-ai", "paradigm"}},
		{Name: "GitHub · Skills & MCP", URL: "github://search/skills-mcp", Tags: []string{"community", "skills", "mcp"}},
		{Name: "GitHub · Community Discussion", URL: "github://search/discussions", Tags: []string{"community", "discussion", "skills", "mcp"}},
		{Name: "GitHub Trending", URL: "https://github.com/trending", Tags: []string{"community", "trending", "skills"}},
	}
}

func Fetch(ctx context.Context, client *http.Client, max int) Result {
	if max <= 0 {
		max = 20
	}
	var result Result
	collect := func(items []rss.Item, err error) {
		result.Items = append(result.Items, items...)
		if err != nil {
			result.Errors = append(result.Errors, err)
		}
	}
	items, err := fetchClaudeChangelog(ctx, client, max)
	collect(items, err)
	items, err = fetchLinkPage(ctx, client, "Anthropic News", "https://www.anthropic.com/news", "https://www.anthropic.com", "/news/", []string{"primary", "anthropic", "claude"}, max)
	collect(items, err)
	items, err = fetchHeadingPage(ctx, client, "ChatGPT Release Notes", "https://help.openai.com/en/articles/6825453-chatgpt-release-notes", []string{"primary", "openai", "codex"}, max)
	collect(items, err)
	items, err = fetchHeadingPage(ctx, client, "Model Release Notes", "https://help.openai.com/en/articles/9624314-model-release-notes", []string{"primary", "openai", "model-infra", "codex"}, max)
	collect(items, err)
	items, err = fetchBluesky(ctx, client, max)
	collect(items, err)
	items, err = fetchGitHub(ctx, max)
	collect(items, err)
	items, err = fetchGitHubDiscussions(ctx, max)
	collect(items, err)
	items, err = fetchTrending(ctx, client, max)
	collect(items, err)
	return result
}

func fetchClaudeChangelog(ctx context.Context, client *http.Client, max int) ([]rss.Item, error) {
	const raw = "https://raw.githubusercontent.com/anthropics/claude-code/main/CHANGELOG.md"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Claude Code changelog: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Claude Code changelog: %s", resp.Status)
	}
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(strings.TrimSpace(string(payload)), "<") {
		return nil, fmt.Errorf("Claude Code changelog returned HTML")
	}
	sections := strings.Split(string(payload), "\n## ")
	items := make([]rss.Item, 0, min(max, len(sections)))
	for _, section := range sections[1:] {
		lines := strings.SplitN(section, "\n", 2)
		if len(lines) < 2 {
			continue
		}
		version, body := strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1])
		if version == "" || body == "" {
			continue
		}
		items = append(items, rss.Item{FeedName: "Claude Code Changelog", FeedURL: raw, FeedTags: []string{"primary", "claude-code", "codex-transfer"}, Title: "Claude Code " + version, Link: "https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md", Summary: body, Content: body})
		if len(items) >= max {
			break
		}
	}
	return items, nil
}

func fetchLinkPage(ctx context.Context, client *http.Client, name, pageURL, base, contains string, tags []string, max int) ([]rss.Item, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	req.Header.Set("User-Agent", "rss-agent/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", name, resp.Status)
	}
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var items []rss.Item
	doc.Find("a[href]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		href, _ := s.Attr("href")
		title := strings.Join(strings.Fields(s.Text()), " ")
		if !strings.Contains(href, contains) || len([]rune(title)) < 12 {
			return true
		}
		link, err := url.Parse(href)
		if err != nil {
			return true
		}
		if !link.IsAbs() {
			root, _ := url.Parse(base)
			link = root.ResolveReference(link)
		}
		canonical := link.String()
		if seen[canonical] {
			return true
		}
		seen[canonical] = true
		items = append(items, rss.Item{FeedName: name, FeedURL: pageURL, FeedTags: tags, Title: title, Link: canonical, Summary: title})
		return len(items) < max
	})
	return items, nil
}

func fetchHeadingPage(ctx context.Context, client *http.Client, name, pageURL string, tags []string, max int) ([]rss.Item, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	req.Header.Set("User-Agent", "rss-agent/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", name, resp.Status)
	}
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}
	var items []rss.Item
	doc.Find("h2, h3").EachWithBreak(func(_ int, heading *goquery.Selection) bool {
		title := strings.Join(strings.Fields(heading.Text()), " ")
		if len([]rune(title)) < 8 {
			return true
		}
		var body []string
		for node := heading.Next(); node.Length() > 0 && !strings.HasPrefix(goquery.NodeName(node), "h"); node = node.Next() {
			text := strings.Join(strings.Fields(node.Text()), " ")
			if text != "" {
				body = append(body, text)
			}
			if len(strings.Join(body, " ")) > 3000 {
				break
			}
		}
		content := strings.Join(body, " ")
		if content == "" {
			return true
		}
		items = append(items, rss.Item{FeedName: name, FeedURL: pageURL, FeedTags: tags, Title: title, Link: pageURL, Summary: content, Content: content})
		return len(items) < max
	})
	return items, nil
}

func fetchBluesky(ctx context.Context, client *http.Client, max int) ([]rss.Item, error) {
	queries := []string{"loop engineering AI", "harness engineering agent", "applied AI engineering", "Codex skill MCP"}
	var items []rss.Item
	seen := map[string]bool{}
	for _, query := range queries {
		endpoint := "https://api.bsky.app/xrpc/app.bsky.feed.searchPosts?limit=10&q=" + url.QueryEscape(query)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		resp, err := client.Do(req)
		if err != nil {
			return items, err
		}
		var body struct {
			Posts []struct {
				URI    string `json:"uri"`
				Author struct {
					Handle string `json:"handle"`
				} `json:"author"`
				Record struct {
					Text      string `json:"text"`
					CreatedAt string `json:"createdAt"`
				} `json:"record"`
				LikeCount   int `json:"likeCount"`
				ReplyCount  int `json:"replyCount"`
				RepostCount int `json:"repostCount"`
			} `json:"posts"`
		}
		err = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if err != nil {
			return items, err
		}
		for _, post := range body.Posts {
			parts := strings.Split(post.URI, "/")
			if len(parts) == 0 || seen[post.URI] {
				continue
			}
			seen[post.URI] = true
			text := strings.TrimSpace(post.Record.Text)
			if len([]rune(text)) < 80 || post.LikeCount+post.ReplyCount+post.RepostCount < 3 {
				continue
			}
			published, _ := time.Parse(time.RFC3339, post.Record.CreatedAt)
			link := fmt.Sprintf("https://bsky.app/profile/%s/post/%s", post.Author.Handle, parts[len(parts)-1])
			items = append(items, rss.Item{FeedName: "Bluesky · AI Builders", FeedURL: "https://bsky.app", FeedTags: []string{"community", "applied-ai", "paradigm"}, Title: firstLine(text), Link: link, Author: post.Author.Handle, PublishedAt: published, Summary: text, Content: text})
			if len(items) >= max {
				return items, nil
			}
		}
	}
	return items, nil
}

func fetchGitHub(ctx context.Context, max int) ([]rss.Item, error) {
	var repos []struct {
		Name        string `json:"fullName"`
		Description string `json:"description"`
		URL         string `json:"url"`
		Stars       int    `json:"stargazersCount"`
		Updated     string `json:"updatedAt"`
		Language    string `json:"language"`
	}
	seen := map[string]bool{}
	cutoff := time.Now().AddDate(0, -2, 0).Format("2006-01-02")
	for _, topic := range []string{"mcp", "codex", "ai-agents"} {
		cmd := exec.CommandContext(ctx, "gh", "search", "repos", "topic:"+topic+" pushed:>"+cutoff, "--sort", "stars", "--order", "desc", "--limit", fmt.Sprint(max), "--json", "fullName,description,url,stargazersCount,updatedAt,language")
		data, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("gh search: %w", err)
		}
		var found []struct {
			Name        string `json:"fullName"`
			Description string `json:"description"`
			URL         string `json:"url"`
			Stars       int    `json:"stargazersCount"`
			Updated     string `json:"updatedAt"`
			Language    string `json:"language"`
		}
		if err := json.Unmarshal(data, &found); err != nil {
			return nil, err
		}
		for _, repo := range found {
			if !seen[repo.URL] {
				seen[repo.URL] = true
				repos = append(repos, repo)
			}
		}
	}
	sort.SliceStable(repos, func(i, j int) bool { return repos[i].Stars > repos[j].Stars })
	if len(repos) > max {
		repos = repos[:max]
	}
	items := make([]rss.Item, 0, len(repos))
	for _, repo := range repos {
		updated, _ := time.Parse(time.RFC3339, repo.Updated)
		summary := fmt.Sprintf("%d stars · %s · %s", repo.Stars, repo.Language, repo.Description)
		items = append(items, rss.Item{FeedName: "GitHub · Skills & MCP", FeedURL: "github://search/skills-mcp", FeedTags: []string{"community", "skills", "mcp"}, Title: repo.Name, Link: repo.URL, UpdatedAt: updated, Summary: summary, Content: summary})
	}
	return items, nil
}

func fetchTrending(ctx context.Context, client *http.Client, max int) ([]rss.Item, error) {
	var items []rss.Item
	for _, period := range []string{"daily", "weekly"} {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://github.com/trending?since="+period, nil)
		req.Header.Set("User-Agent", "rss-agent/1.0")
		resp, err := client.Do(req)
		if err != nil {
			return items, err
		}
		doc, err := goquery.NewDocumentFromReader(resp.Body)
		resp.Body.Close()
		if err != nil {
			return items, err
		}
		doc.Find("article.Box-row").EachWithBreak(func(_ int, s *goquery.Selection) bool {
			href, _ := s.Find("h2 a").Attr("href")
			title := strings.Join(strings.Fields(s.Find("h2").Text()), "")
			description := strings.Join(strings.Fields(s.Find("p").Text()), " ")
			if href != "" {
				items = append(items, rss.Item{FeedName: "GitHub Trending · " + period, FeedURL: "https://github.com/trending", FeedTags: []string{"community", "trending", "skills"}, Title: title, Link: "https://github.com" + href, Summary: description, Content: description})
			}
			return len(items) < max
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].FeedName < items[j].FeedName })
	return items, nil
}

func fetchGitHubDiscussions(ctx context.Context, max int) ([]rss.Item, error) {
	type issue struct {
		Title       string `json:"title"`
		URL         string `json:"url"`
		Body        string `json:"body"`
		Updated     string `json:"updatedAt"`
		Association string `json:"authorAssociation"`
		Comments    int    `json:"commentsCount"`
		IsPR        bool   `json:"isPullRequest"`
	}
	cutoff := time.Now().AddDate(0, -1, 0).Format("2006-01-02")
	seen := map[string]bool{}
	var items []rss.Item
	for _, term := range []string{"mcp", "codex skill", "agent engineering"} {
		cmd := exec.CommandContext(ctx, "gh", "search", "issues", term+" updated:>"+cutoff+" comments:>=5", "--match", "title", "--sort", "comments", "--order", "desc", "--limit", fmt.Sprint(max), "--json", "title,url,body,commentsCount,updatedAt,authorAssociation,isPullRequest")
		data, err := cmd.Output()
		if err != nil {
			return items, fmt.Errorf("gh issue search: %w", err)
		}
		var found []issue
		if err := json.Unmarshal(data, &found); err != nil {
			return items, err
		}
		for _, value := range found {
			if seen[value.URL] || value.Comments > 500 || value.Association == "OWNER" || value.Association == "MEMBER" || value.Association == "COLLABORATOR" {
				continue
			}
			seen[value.URL] = true
			updated, _ := time.Parse(time.RFC3339, value.Updated)
			kind := "Issue"
			if value.IsPR {
				kind = "PR"
			}
			summary := fmt.Sprintf("%s · %d 条评论 · %s", kind, value.Comments, strings.TrimSpace(value.Body))
			items = append(items, rss.Item{FeedName: "GitHub · Community Discussion", FeedURL: "github://search/discussions", FeedTags: []string{"community", "discussion", "skills", "mcp"}, Title: value.Title, Link: value.URL, UpdatedAt: updated, Summary: summary, Content: summary})
			if len(items) >= max {
				return items, nil
			}
		}
	}
	return items, nil
}

func firstLine(value string) string {
	line := strings.SplitN(value, "\n", 2)[0]
	runes := []rune(line)
	if len(runes) > 100 {
		runes = runes[:100]
	}
	return string(runes)
}
