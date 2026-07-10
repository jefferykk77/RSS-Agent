package push

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/jeffery/rss-agent/internal/agent"
)

const defaultTelegramAPIBaseURL = "https://api.telegram.org"

type Pusher struct {
	Console            bool
	WebhookURL         string
	FeishuWebhookURL   string
	DingTalkWebhookURL string
	TelegramBotToken   string
	TelegramChatID     string
	TelegramAPIBaseURL string
	Email              EmailConfig
	Client             *http.Client
}

type EmailConfig struct {
	SMTPHost string
	SMTPPort int
	Username string
	Password string
	From     string
	To       []string
	Subject  string
	StartTLS bool
}

type DeliveryResult struct {
	Channel string
	Err     error
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

func (p *Pusher) Push(ctx context.Context, results []agent.Result) ([]DeliveryResult, error) {
	if len(results) == 0 {
		return nil, nil
	}

	text := FormatMarkdown(results)
	deliveries := make([]DeliveryResult, 0, 5)
	var deliveryErrors []error
	run := func(channel string, send func() error) {
		err := send()
		deliveries = append(deliveries, DeliveryResult{Channel: channel, Err: err})
		if err != nil {
			deliveryErrors = append(deliveryErrors, fmt.Errorf("%s: %w", channel, err))
		}
	}

	if p.Console {
		run("console", func() error {
			fmt.Print(text)
			return nil
		})
	}
	if p.WebhookURL != "" {
		run("webhook", func() error { return p.postWebhook(ctx, text, results) })
	}
	if p.FeishuWebhookURL != "" {
		run("feishu", func() error { return p.postFeishu(ctx, text) })
	}
	if p.DingTalkWebhookURL != "" {
		run("dingtalk", func() error { return p.postDingTalk(ctx, text) })
	}
	if p.TelegramBotToken != "" && p.TelegramChatID != "" {
		run("telegram", func() error { return p.postTelegram(ctx, text) })
	}
	if p.Email.SMTPHost != "" {
		run("email", func() error { return p.sendEmail(ctx, text) })
	}

	if len(deliveries) == 0 {
		return nil, errors.New("no push channel configured")
	}
	return deliveries, errors.Join(deliveryErrors...)
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
	return p.postJSON(ctx, "webhook", p.WebhookURL, WebhookPayload{Text: text, Items: items})
}

func (p *Pusher) postFeishu(ctx context.Context, text string) error {
	payload := struct {
		MessageType string `json:"msg_type"`
		Content     struct {
			Text string `json:"text"`
		} `json:"content"`
	}{MessageType: "text"}
	payload.Content.Text = text
	return p.postJSON(ctx, "feishu", p.FeishuWebhookURL, payload)
}

func (p *Pusher) postDingTalk(ctx context.Context, text string) error {
	payload := struct {
		MessageType string `json:"msgtype"`
		Markdown    struct {
			Title string `json:"title"`
			Text  string `json:"text"`
		} `json:"markdown"`
	}{MessageType: "markdown"}
	payload.Markdown.Title = "RSS Agent"
	payload.Markdown.Text = text
	return p.postJSON(ctx, "dingtalk", p.DingTalkWebhookURL, payload)
}

func (p *Pusher) postTelegram(ctx context.Context, text string) error {
	baseURL := strings.TrimRight(p.TelegramAPIBaseURL, "/")
	if baseURL == "" {
		baseURL = defaultTelegramAPIBaseURL
	}
	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", baseURL, p.TelegramBotToken)
	payload := struct {
		ChatID                string `json:"chat_id"`
		Text                  string `json:"text"`
		ParseMode             string `json:"parse_mode"`
		DisableWebPagePreview bool   `json:"disable_web_page_preview"`
	}{
		ChatID:                p.TelegramChatID,
		Text:                  text,
		ParseMode:             "Markdown",
		DisableWebPagePreview: true,
	}
	return p.postJSON(ctx, "telegram", endpoint, payload)
}

func (p *Pusher) postJSON(ctx context.Context, channel string, endpoint string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return errors.New("create request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("%s request failed", channel)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("received HTTP status %s", resp.Status)
	}
	return nil
}

func (p *Pusher) sendEmail(ctx context.Context, text string) error {
	config := p.Email
	to := cleanAddresses(config.To)
	from := firstNonEmpty(config.From, config.Username)
	if config.SMTPHost == "" || from == "" || len(to) == 0 {
		return errors.New("smtp host, sender, and recipient are required")
	}
	port := config.SMTPPort
	if port <= 0 {
		port = 587
	}

	dialer := &net.Dialer{Timeout: 15 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(config.SMTPHost, strconv.Itoa(port)))
	if err != nil {
		return errors.New("connect to SMTP server")
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(15 * time.Second))

	client, err := smtp.NewClient(connection, config.SMTPHost)
	if err != nil {
		return errors.New("start SMTP session")
	}
	defer client.Quit()
	if config.StartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP server does not support STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: config.SMTPHost}); err != nil {
			return errors.New("start TLS")
		}
	}
	if config.Username != "" || config.Password != "" {
		if err := client.Auth(smtp.PlainAuth("", config.Username, config.Password, config.SMTPHost)); err != nil {
			return errors.New("SMTP authentication failed")
		}
	}
	if err := client.Mail(from); err != nil {
		return errors.New("set email sender")
	}
	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return errors.New("set email recipient")
		}
	}
	body, err := client.Data()
	if err != nil {
		return errors.New("start email body")
	}
	if _, err := body.Write(buildEmailMessage(from, to, config.Subject, text)); err != nil {
		_ = body.Close()
		return errors.New("write email body")
	}
	if err := body.Close(); err != nil {
		return errors.New("finish email body")
	}
	return nil
}

func (p *Pusher) httpClient() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func buildEmailMessage(from string, to []string, subject string, text string) []byte {
	if subject = firstNonEmpty(subject, "RSS Agent Digest"); subject != "" {
		subject = mime.QEncoding.Encode("UTF-8", subject)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", cleanHeaderValue(from))
	fmt.Fprintf(&b, "To: %s\r\n", cleanHeaderValue(strings.Join(to, ", ")))
	fmt.Fprintf(&b, "Subject: %s\r\n", cleanHeaderValue(subject))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(text)
	return []byte(b.String())
}

func cleanAddresses(addresses []string) []string {
	cleaned := make([]string, 0, len(addresses))
	for _, address := range addresses {
		if address = strings.TrimSpace(address); address != "" {
			cleaned = append(cleaned, address)
		}
	}
	return cleaned
}

func cleanHeaderValue(value string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(strings.TrimSpace(value))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
