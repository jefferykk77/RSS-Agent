package ingest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

type Page struct {
	Title       string
	Description string
	Content     string
}

func Fetch(ctx context.Context, client *http.Client, rawURL string, maxBytes int64) (Page, error) {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(rawURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return Page{}, fmt.Errorf("链接必须是有效的 http 或 https 地址")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return Page{}, err
	}
	request.Header.Set("User-Agent", "RSS-Agent/1.0")
	response, err := client.Do(request)
	if err != nil {
		return Page{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Page{}, fmt.Errorf("页面返回 %s", response.Status)
	}
	if maxBytes <= 0 {
		maxBytes = 2 << 20
	}
	document, err := html.Parse(io.LimitReader(response.Body, maxBytes))
	if err != nil {
		return Page{}, err
	}
	page := Page{}
	var paragraphs []string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "script", "style", "nav", "footer", "svg":
				return
			case "meta":
				property, content := attr(node, "property"), attr(node, "content")
				name := attr(node, "name")
				if page.Title == "" && (property == "og:title" || name == "twitter:title") {
					page.Title = content
				}
				if page.Description == "" && (property == "og:description" || name == "description" || name == "twitter:description") {
					page.Description = content
				}
			case "title":
				if page.Title == "" {
					page.Title = nodeText(node)
				}
			case "p", "li":
				if text := nodeText(node); len([]rune(text)) >= 24 {
					paragraphs = append(paragraphs, text)
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	page.Title = strings.TrimSpace(page.Title)
	page.Description = strings.TrimSpace(page.Description)
	page.Content = strings.Join(paragraphs, "\n\n")
	return page, nil
}

func attr(node *html.Node, key string) string {
	for _, attribute := range node.Attr {
		if strings.EqualFold(attribute.Key, key) {
			return strings.TrimSpace(attribute.Val)
		}
	}
	return ""
}

func nodeText(node *html.Node) string {
	var values []string
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			values = append(values, current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return strings.Join(strings.Fields(strings.Join(values, " ")), " ")
}
