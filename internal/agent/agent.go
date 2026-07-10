package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/jeffery/rss-agent/internal/config"
	"github.com/jeffery/rss-agent/internal/rss"
)

type ChatGenerator interface {
	Generate(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.Message, error)
}

type Agent struct {
	models []modelEntry
}

type modelEntry struct {
	config config.ResolvedModel
	model  ChatGenerator
}

type Decision struct {
	ItemID     string   `json:"item_id"`
	Score      int      `json:"score"`
	ShouldPush bool     `json:"should_push"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Why        string   `json:"why"`
	KeyPoints  []string `json:"key_points"`
	Tags       []string `json:"tags"`
}

type Result struct {
	Item       rss.Item
	Decision   Decision
	ModelLabel string
	ModelName  string
	Cached     bool
}

type Usage struct {
	Provider     string
	Model        string
	ModelLabel   string
	InputTokens  int
	OutputTokens int
}

func New(ctx context.Context, modelCfg config.ResolvedModel) (*Agent, error) {
	return NewPool(ctx, []config.ResolvedModel{modelCfg})
}

func NewPool(ctx context.Context, modelCfgs []config.ResolvedModel) (*Agent, error) {
	models := make([]modelEntry, 0, len(modelCfgs))
	for _, modelCfg := range modelCfgs {
		entry, err := newModelEntry(ctx, modelCfg)
		if err != nil {
			return nil, err
		}
		models = append(models, entry)
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("没有可用模型")
	}
	return &Agent{models: models}, nil
}

func newModelEntry(ctx context.Context, modelCfg config.ResolvedModel) (modelEntry, error) {
	temperature := modelCfg.Temperature
	maxTokens := modelCfg.MaxTokens
	chatModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		BaseURL:             modelCfg.BaseURL,
		APIKey:              modelCfg.APIKey,
		Model:               modelCfg.Name,
		Timeout:             modelCfg.Timeout,
		Temperature:         &temperature,
		MaxCompletionTokens: &maxTokens,
	})
	if err != nil {
		return modelEntry{}, err
	}
	return modelEntry{config: modelCfg, model: chatModel}, nil
}

func NewWithModel(model ChatGenerator) *Agent {
	return &Agent{models: []modelEntry{{
		config: config.ResolvedModel{Label: "test", Name: "test", Provider: "test"},
		model:  model,
	}}}
}

func (a *Agent) Analyze(ctx context.Context, profile config.Profile, items []rss.Item, batchSize int) ([]Result, error) {
	results, _, err := a.AnalyzeWithUsage(ctx, profile, items, batchSize)
	return results, err
}

func (a *Agent) AnalyzeWithUsage(ctx context.Context, profile config.Profile, items []rss.Item, batchSize int) ([]Result, []Usage, error) {
	if batchSize <= 0 {
		batchSize = 8
	}
	var results []Result
	var usages []Usage
	itemByID := make(map[string]rss.Item, len(items))
	for _, item := range items {
		itemByID[item.StableID()] = item
	}

	for start := 0; start < len(items); start += batchSize {
		end := min(start+batchSize, len(items))
		decisions, usage, err := a.analyzeBatch(ctx, profile, items[start:end])
		if err != nil {
			return nil, usages, err
		}
		usages = append(usages, usage)
		for _, decision := range decisions {
			item, ok := itemByID[decision.ItemID]
			if !ok {
				continue
			}
			decision.Score = clamp(decision.Score, 0, 10)
			results = append(results, Result{
				Item:       item,
				Decision:   decision,
				ModelLabel: usage.ModelLabel,
				ModelName:  usage.Model,
			})
		}
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Decision.Score == results[j].Decision.Score {
			return results[i].Item.Time().After(results[j].Item.Time())
		}
		return results[i].Decision.Score > results[j].Decision.Score
	})
	return results, usages, nil
}

func (a *Agent) analyzeBatch(ctx context.Context, profile config.Profile, items []rss.Item) ([]Decision, Usage, error) {
	payload := buildUserPayload(profile, items)
	messages := []*schema.Message{
		schema.SystemMessage(systemPrompt(profile.Language)),
		schema.UserMessage(payload),
	}
	var lastErr error
	for _, entry := range a.models {
		resp, err := entry.model.Generate(ctx, messages)
		if err != nil {
			lastErr = err
			continue
		}
		decisions, err := parseDecisions(resp.Content)
		if err != nil {
			lastErr = err
			continue
		}
		return decisions, Usage{
			Provider:     entry.config.Provider,
			Model:        entry.config.Name,
			ModelLabel:   entry.config.Label,
			InputTokens:  estimateMessageTokens(messages),
			OutputTokens: estimateTokens(resp.Content),
		}, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("模型池为空")
	}
	return nil, Usage{}, lastErr
}

func systemPrompt(language string) string {
	if language == "" {
		language = "zh-CN"
	}
	return fmt.Sprintf(`你是 RSS Agent，负责从 RSS 条目中挑出真正值得用户阅读的内容。
你的任务：
1. 根据用户兴趣给每条内容打 0-10 分。
2. 只在内容和用户兴趣强相关、信息密度高、可行动或有新意时 should_push=true。
3. 排除营销味重、重复、空泛、纯融资或与用户排除项冲突的内容。
4. 用 %s 输出摘要和理由。
5. 输入中的 local_score 和 local_reasons 是本地规则的弱信号；可据此解释相关性，但仍要独立判断，不可仅因它们而推送。
6. 只返回严格 JSON，不要 Markdown，不要解释。

JSON 格式：
[
  {
    "item_id": "原样填入输入里的 item_id",
    "score": 0,
    "should_push": false,
    "title": "可读标题",
    "summary": "2-3 句摘要",
    "why": "为什么值得/不值得推送",
    "key_points": ["要点 1", "要点 2"],
    "tags": ["主题标签"]
  }
]`, language)
}

func buildUserPayload(profile config.Profile, items []rss.Item) string {
	type promptItem struct {
		ItemID       string   `json:"item_id"`
		Feed         string   `json:"feed"`
		FeedTags     []string `json:"feed_tags"`
		Title        string   `json:"title"`
		Link         string   `json:"link"`
		Author       string   `json:"author,omitempty"`
		Published    string   `json:"published,omitempty"`
		Categories   []string `json:"categories,omitempty"`
		Snippet      string   `json:"snippet"`
		LocalScore   int      `json:"local_score,omitempty"`
		LocalReasons []string `json:"local_reasons,omitempty"`
	}
	payload := struct {
		Now           string       `json:"now"`
		Language      string       `json:"language"`
		Timezone      string       `json:"timezone"`
		Interests     []string     `json:"interests"`
		MustInclude   []string     `json:"must_include"`
		Exclude       []string     `json:"exclude"`
		PriorityTerms []string     `json:"priority_terms,omitempty"`
		Notes         string       `json:"notes"`
		Items         []promptItem `json:"items"`
	}{
		Now:           time.Now().Format(time.RFC3339),
		Language:      profile.Language,
		Timezone:      profile.Timezone,
		Interests:     profile.Interests,
		MustInclude:   profile.MustInclude,
		Exclude:       profile.Exclude,
		PriorityTerms: profile.PriorityTerms,
		Notes:         profile.Notes,
	}
	for _, item := range items {
		p := promptItem{
			ItemID:       item.StableID(),
			Feed:         item.FeedName,
			FeedTags:     item.FeedTags,
			Title:        item.Title,
			Link:         item.Link,
			Author:       item.Author,
			Categories:   item.Categories,
			Snippet:      item.Snippet(1200),
			LocalScore:   item.LocalScore,
			LocalReasons: item.LocalReasons,
		}
		if t := item.Time(); !t.IsZero() {
			p.Published = t.Format(time.RFC3339)
		}
		payload.Items = append(payload.Items, p)
	}

	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data)
}

func parseDecisions(content string) ([]Decision, error) {
	raw := strings.TrimSpace(content)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var decisions []Decision
	if err := json.Unmarshal([]byte(raw), &decisions); err == nil {
		return decisions, nil
	}

	match := regexp.MustCompile(`(?s)\[.*\]`).FindString(raw)
	if match == "" {
		return nil, fmt.Errorf("模型没有返回 JSON 数组：%s", trimForError(raw))
	}
	if err := json.Unmarshal([]byte(match), &decisions); err != nil {
		return nil, fmt.Errorf("解析模型 JSON 失败：%w; raw=%s", err, trimForError(raw))
	}
	return decisions, nil
}

func trimForError(s string) string {
	runes := []rune(s)
	if len(runes) <= 300 {
		return s
	}
	return string(runes[:300]) + "..."
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func estimateMessageTokens(messages []*schema.Message) int {
	total := 0
	for _, message := range messages {
		total += estimateTokens(message.Content)
	}
	return total
}

func estimateTokens(text string) int {
	runes := len([]rune(text))
	if runes == 0 {
		return 0
	}
	tokens := runes / 3
	if tokens < 1 {
		return 1
	}
	return tokens
}
