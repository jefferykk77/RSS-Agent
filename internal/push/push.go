package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jeffery/rss-agent/internal/agent"
)

type Pusher struct {
	Console    bool
	WebhookURL string
	Client     *http.Client
}

type WebhookPayload struct {
	Text  string        `json:"text"`
	Items []WebhookItem `json:"items"`
}

type WebhookItem struct {
	Title     string   `json:"title"`
	Link      string   `json:"link"`
	Feed      string   `json:"feed"`
	Score     int      `json:"score"`
	Summary   string   `json:"summary"`
	Why       string   `json:"why"`
	KeyPoints []string `json:"key_points"`
	Tags      []string `json:"tags"`
}

func (p *Pusher) Push(ctx context.Context, results []agent.Result) error {
	if len(results) == 0 {
		return nil
	}
	text := FormatMarkdown(results)
	if p.Console {
		fmt.Println(text)
	}
	if p.WebhookURL != "" {
		return p.postWebhook(ctx, text, results)
	}
	return nil
}

func FormatMarkdown(results []agent.Result) string {
	var b strings.Builder
	b.WriteString("# RSS Agent\n\n")
	b.WriteString(fmt.Sprintf("共 %d 条值得看。\n\n", len(results)))
	for _, result := range results {
		item := result.Item
		decision := result.Decision
		title := firstNonEmpty(decision.Title, item.Title)
		b.WriteString(fmt.Sprintf("## [%s](%s)\n", title, item.Link))
		b.WriteString(fmt.Sprintf("- 来源：%s\n", item.FeedName))
		b.WriteString(fmt.Sprintf("- 分数：%d/10\n", decision.Score))
		if !item.Time().IsZero() {
			b.WriteString(fmt.Sprintf("- 时间：%s\n", item.Time().Format(time.RFC3339)))
		}
		if decision.Summary != "" {
			b.WriteString(fmt.Sprintf("- 摘要：%s\n", decision.Summary))
		}
		if decision.Why != "" {
			b.WriteString(fmt.Sprintf("- 理由：%s\n", decision.Why))
		}
		if len(decision.KeyPoints) > 0 {
			b.WriteString("- 要点：")
			b.WriteString(strings.Join(decision.KeyPoints, "；"))
			b.WriteString("\n")
		}
		if len(decision.Tags) > 0 {
			b.WriteString("- 标签：")
			b.WriteString(strings.Join(decision.Tags, ", "))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (p *Pusher) postWebhook(ctx context.Context, text string, results []agent.Result) error {
	items := make([]WebhookItem, 0, len(results))
	for _, result := range results {
		items = append(items, WebhookItem{
			Title:     firstNonEmpty(result.Decision.Title, result.Item.Title),
			Link:      result.Item.Link,
			Feed:      result.Item.FeedName,
			Score:     result.Decision.Score,
			Summary:   result.Decision.Summary,
			Why:       result.Decision.Why,
			KeyPoints: result.Decision.KeyPoints,
			Tags:      result.Decision.Tags,
		})
	}
	payload := WebhookPayload{Text: text, Items: items}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.WebhookURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook 返回非 2xx 状态：%s", resp.Status)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
