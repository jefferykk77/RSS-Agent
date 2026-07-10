package rss

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"golang.org/x/net/html"
)

const (
	fullTextMaxBytes = 2 << 20
	fullTextWorkers  = 4
)

type FullTextResult struct {
	Items []Item
	Errs  []error
}

type fullTextOutcome struct {
	index   int
	content string
	err     error
}

func (f *Fetcher) EnrichFullText(ctx context.Context, items []Item, minChars, maxChars int) FullTextResult {
	result := FullTextResult{Items: append([]Item(nil), items...)}
	if minChars <= 0 || maxChars <= 0 {
		return result
	}
	if maxChars < minChars {
		maxChars = minChars
	}

	indexes := make([]int, 0, len(result.Items))
	for index, item := range result.Items {
		if strings.TrimSpace(item.Link) == "" || len([]rune(item.Text())) >= minChars {
			continue
		}
		indexes = append(indexes, index)
	}
	if len(indexes) == 0 {
		return result
	}

	jobs := make(chan int)
	outcomes := make(chan fullTextOutcome, len(indexes))
	workers := min(fullTextWorkers, len(indexes))
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				content, err := f.fetchFullText(ctx, result.Items[index].Link, maxChars)
				outcomes <- fullTextOutcome{index: index, content: content, err: err}
			}
		}()
	}
	go func() {
		for _, index := range indexes {
			jobs <- index
		}
		close(jobs)
		wg.Wait()
		close(outcomes)
	}()

	for outcome := range outcomes {
		if outcome.err != nil {
			result.Errs = append(result.Errs, fmt.Errorf("正文抓取 %s：%w", result.Items[outcome.index].Link, outcome.err))
			continue
		}
		if outcome.content != "" {
			result.Items[outcome.index].Content = outcome.content
		}
	}
	return result
}

func (f *Fetcher) fetchFullText(ctx context.Context, rawURL string, maxChars int) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if parsedURL.Host == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return "", fmt.Errorf("只支持 http(s) 正文链接")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", f.userAgent)
	resp, err := f.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("HTTP %s", resp.Status)
	}
	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || (mediaType != "text/html" && mediaType != "application/xhtml+xml") {
			return "", fmt.Errorf("响应不是 HTML 页面")
		}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, fullTextMaxBytes+1))
	if err != nil {
		return "", err
	}
	if len(body) > fullTextMaxBytes {
		return "", fmt.Errorf("页面超过 %d MiB 限制", fullTextMaxBytes>>20)
	}
	return extractFullText(bytes.NewReader(body), maxChars)
}

func extractFullText(source io.Reader, maxChars int) (string, error) {
	doc, err := html.Parse(source)
	if err != nil {
		return "", err
	}

	var text strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			tag := strings.ToLower(node.Data)
			if ignoredFullTextTag(tag) {
				return
			}
			if fullTextBlockTag(tag) {
				text.WriteByte('\n')
			}
		}
		if node.Type == html.TextNode {
			text.WriteString(node.Data)
			text.WriteByte(' ')
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	content := cleanText(text.String())
	if content == "" {
		return "", fmt.Errorf("页面没有可提取的正文")
	}
	return truncateText(content, maxChars), nil
}

func ignoredFullTextTag(tag string) bool {
	switch tag {
	case "aside", "footer", "form", "head", "iframe", "nav", "noscript", "script", "style", "svg":
		return true
	default:
		return false
	}
}

func fullTextBlockTag(tag string) bool {
	switch tag {
	case "article", "blockquote", "div", "h1", "h2", "h3", "h4", "li", "main", "p", "pre", "section", "td":
		return true
	default:
		return false
	}
}

func truncateText(text string, maxChars int) string {
	if maxChars <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	return strings.TrimSpace(string(runes[:maxChars]))
}
