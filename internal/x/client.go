package x

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jeffery/rss-agent/internal/config"
	"github.com/jeffery/rss-agent/internal/rss"
)

const defaultBaseURL = "https://api.x.com/2"

type Client struct {
	baseURL string
	bearer  string
	http    *http.Client
}

type Result struct {
	Items []rss.Item
	Errs  []error
}

type searchResponse struct {
	Data     []post `json:"data"`
	Includes struct {
		Users []user `json:"users"`
	} `json:"includes"`
}

type post struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	AuthorID  string `json:"author_id"`
	CreatedAt string `json:"created_at"`
	Entities  struct {
		Hashtags []struct {
			Tag string `json:"tag"`
		} `json:"hashtags"`
	} `json:"entities"`
}

type user struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
}

func New(baseURL, bearer string, client *http.Client) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		bearer:  strings.TrimSpace(bearer),
		http:    client,
	}
}

func (c *Client) FetchSearches(ctx context.Context, searches []config.XSearch) Result {
	result := Result{Items: []rss.Item{}}
	if len(searches) == 0 {
		return result
	}
	if c.bearer == "" {
		result.Errs = append(result.Errs, fmt.Errorf("X search is configured but X_BEARER_TOKEN is missing"))
		return result
	}
	for _, search := range searches {
		items, err := c.FetchSearch(ctx, search)
		if err != nil {
			result.Errs = append(result.Errs, fmt.Errorf("X search %q: %w", search.Name, err))
			continue
		}
		result.Items = append(result.Items, items...)
	}
	return result
}

func (c *Client) FetchSearch(ctx context.Context, search config.XSearch) ([]rss.Item, error) {
	endpoint, err := url.Parse(c.baseURL + "/tweets/search/recent")
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set("query", search.Query)
	query.Set("max_results", fmt.Sprintf("%d", search.MaxResults))
	query.Set("tweet.fields", "created_at,author_id,entities")
	query.Set("expansions", "author_id")
	query.Set("user.fields", "name,username")
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.bearer)
	request.Header.Set("Accept", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 16<<10))
		return nil, fmt.Errorf("X API returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}

	var payload searchResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	users := make(map[string]user, len(payload.Includes.Users))
	for _, author := range payload.Includes.Users {
		users[author.ID] = author
	}

	items := make([]rss.Item, 0, len(payload.Data))
	for _, post := range payload.Data {
		if strings.TrimSpace(post.ID) == "" || strings.TrimSpace(post.Text) == "" {
			continue
		}
		author := users[post.AuthorID]
		item := rss.Item{
			ID:         "x:" + post.ID,
			FeedName:   search.Name,
			FeedURL:    SearchURL(search.Query),
			FeedTags:   append([]string(nil), search.Tags...),
			Title:      postTitle(post.Text),
			Link:       postURL(author.Username, post.ID),
			GUID:       post.ID,
			Author:     firstNonEmpty(author.Name, "@"+author.Username),
			Categories: postTags(post),
			Summary:    post.Text,
			Content:    post.Text,
		}
		item.PublishedAt, _ = time.Parse(time.RFC3339, post.CreatedAt)
		items = append(items, item)
	}
	return items, nil
}

func SearchURL(query string) string {
	return "https://x.com/search?" + url.Values{"q": {query}, "src": {"typed_query"}}.Encode()
}

func postURL(username, postID string) string {
	if strings.TrimSpace(username) == "" {
		return "https://x.com/i/web/status/" + url.PathEscape(postID)
	}
	return "https://x.com/" + url.PathEscape(username) + "/status/" + url.PathEscape(postID)
}

func postTitle(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= 120 {
		return text
	}
	return string(runes[:120]) + "..."
}

func postTags(post post) []string {
	tags := make([]string, 0, len(post.Entities.Hashtags))
	for _, hashtag := range post.Entities.Hashtags {
		if tag := strings.TrimSpace(hashtag.Tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
