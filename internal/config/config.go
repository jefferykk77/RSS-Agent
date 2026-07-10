package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Profile  Profile     `yaml:"profile"`
	Model    Model       `yaml:"model,omitempty"`
	Models   ModelPool   `yaml:"models,omitempty"`
	Settings Settings    `yaml:"settings"`
	Database Database    `yaml:"database"`
	Budget   Budget      `yaml:"budget"`
	Push     Push        `yaml:"push"`
	Feeds    []Feed      `yaml:"feeds"`
	State    StateConfig `yaml:"state,omitempty"`
}

type Profile struct {
	Language      string   `yaml:"language"`
	Timezone      string   `yaml:"timezone"`
	Interests     []string `yaml:"interests"`
	MustInclude   []string `yaml:"must_include"`
	Exclude       []string `yaml:"exclude"`
	PriorityTerms []string `yaml:"priority_terms"`
	MutedFeeds    []string `yaml:"muted_feeds"`
	MutedTags     []string `yaml:"muted_tags"`
	Notes         string   `yaml:"notes"`
}

type Model struct {
	Label                    string  `yaml:"label,omitempty"`
	Provider                 string  `yaml:"provider"`
	BaseURL                  string  `yaml:"base_url"`
	APIKey                   string  `yaml:"api_key"`
	APIKeyEnv                string  `yaml:"api_key_env"`
	Name                     string  `yaml:"name"`
	Timeout                  string  `yaml:"timeout"`
	Temperature              float32 `yaml:"temperature"`
	MaxTokens                int     `yaml:"max_tokens"`
	InputPriceCNYPerMillion  float64 `yaml:"input_price_cny_per_million"`
	OutputPriceCNYPerMillion float64 `yaml:"output_price_cny_per_million"`
	FreeDailyTokens          int     `yaml:"free_daily_tokens"`
	Enabled                  *bool   `yaml:"enabled,omitempty"`
}

type ModelPool struct {
	Primary  Model   `yaml:"primary,omitempty"`
	Fallback []Model `yaml:"fallback,omitempty"`
}

type Settings struct {
	Interval            string `yaml:"interval"`
	HTTPTimeout         string `yaml:"http_timeout"`
	LookbackHours       int    `yaml:"lookback_hours"`
	MaxItemsPerFeed     int    `yaml:"max_items_per_feed"`
	BatchSize           int    `yaml:"batch_size"`
	MinScore            int    `yaml:"min_score"`
	MaxPushes           int    `yaml:"max_pushes"`
	MaxCandidatesPerRun *int   `yaml:"max_candidates_per_run"`
	AnalysisCacheTTL    string `yaml:"analysis_cache_ttl"`
}

type Database struct {
	Path string `yaml:"path"`
}

type Budget struct {
	MonthlyCNY               float64 `yaml:"monthly_cny"`
	LLMMonthlyCNY            float64 `yaml:"llm_monthly_cny"`
	XMonthlyCNY              float64 `yaml:"x_monthly_cny"`
	HardStopCNY              float64 `yaml:"hard_stop_cny"`
	StopWhenFreeQuotaMissing bool    `yaml:"stop_when_free_quota_missing"`
	WarnWhenUsedRatio        float64 `yaml:"warn_when_used_ratio"`
}

type Push struct {
	Console       bool   `yaml:"console"`
	WebhookURL    string `yaml:"webhook_url"`
	WebhookURLEnv string `yaml:"webhook_url_env"`
}

type Feed struct {
	Name     string   `yaml:"name"`
	URL      string   `yaml:"url"`
	Tags     []string `yaml:"tags"`
	Disabled bool     `yaml:"disabled"`
}

type StateConfig struct {
	Path string `yaml:"path"`
}

type ResolvedModel struct {
	Label                    string
	Provider                 string
	BaseURL                  string
	APIKey                   string
	Name                     string
	Timeout                  time.Duration
	Temperature              float32
	MaxTokens                int
	InputPriceCNYPerMillion  float64
	OutputPriceCNYPerMillion float64
	FreeDailyTokens          int
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	expanded := os.ExpandEnv(string(data))
	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, err
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func Save(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (c *Config) ApplyDefaults() {
	if c.Profile.Language == "" {
		c.Profile.Language = "zh-CN"
	}
	if c.Profile.Timezone == "" {
		c.Profile.Timezone = "Asia/Shanghai"
	}
	c.Model = applyModelDefaults(c.Model)
	c.Models.Primary = applyModelDefaults(c.Models.Primary)
	for i := range c.Models.Fallback {
		c.Models.Fallback[i] = applyModelDefaults(c.Models.Fallback[i])
	}
	if c.Settings.Interval == "" {
		c.Settings.Interval = "30m"
	}
	if c.Settings.HTTPTimeout == "" {
		c.Settings.HTTPTimeout = "20s"
	}
	if c.Settings.LookbackHours == 0 {
		c.Settings.LookbackHours = 72
	}
	if c.Settings.MaxItemsPerFeed == 0 {
		c.Settings.MaxItemsPerFeed = 20
	}
	if c.Settings.BatchSize == 0 {
		c.Settings.BatchSize = 8
	}
	if c.Settings.MinScore == 0 {
		c.Settings.MinScore = 7
	}
	if c.Settings.MaxPushes == 0 {
		c.Settings.MaxPushes = 8
	}
	if c.Settings.AnalysisCacheTTL == "" {
		c.Settings.AnalysisCacheTTL = "168h"
	}
	if c.Database.Path == "" {
		c.Database.Path = ".rss-agent/rss-agent.db"
	}
	if c.Budget.MonthlyCNY == 0 {
		c.Budget.MonthlyCNY = 20
	}
	if c.Budget.LLMMonthlyCNY == 0 {
		c.Budget.LLMMonthlyCNY = 5
	}
	if c.Budget.HardStopCNY == 0 {
		c.Budget.HardStopCNY = c.Budget.MonthlyCNY
	}
	if c.Budget.WarnWhenUsedRatio == 0 {
		c.Budget.WarnWhenUsedRatio = 0.8
	}
	if c.State.Path == "" {
		c.State.Path = ".rss-agent/state.json"
	}
}

func (c *Config) Validate() error {
	if len(c.Profile.Interests) == 0 {
		return errors.New("profile.interests 至少需要一条兴趣描述")
	}
	for i, feed := range c.Feeds {
		if feed.Name == "" {
			return fmt.Errorf("feeds[%d].name 不能为空", i)
		}
		if feed.URL == "" {
			return fmt.Errorf("feeds[%d].url 不能为空", i)
		}
	}
	if c.Settings.MaxCandidatesPerRun != nil && *c.Settings.MaxCandidatesPerRun < 0 {
		return errors.New("settings.max_candidates_per_run cannot be negative")
	}
	for _, model := range c.ModelCandidates() {
		if model.Name == "" || isDisabled(model) {
			continue
		}
		if !isSupportedProvider(model.Provider) {
			return fmt.Errorf("目前支持 openai-compatible provider，收到 %q", model.Provider)
		}
	}
	return nil
}

func (c *Config) Interval() time.Duration {
	return mustDuration(c.Settings.Interval, 30*time.Minute)
}

func (c *Config) HTTPTimeout() time.Duration {
	return mustDuration(c.Settings.HTTPTimeout, 20*time.Second)
}

func (c *Config) AnalysisCacheTTL() time.Duration {
	return mustDuration(c.Settings.AnalysisCacheTTL, 168*time.Hour)
}

func (s Settings) CandidateLimit() int {
	if s.MaxCandidatesPerRun == nil {
		return 24
	}
	return *s.MaxCandidatesPerRun
}

func (c *Config) DatabasePath() string {
	if c.Database.Path != "" {
		return c.Database.Path
	}
	return ".rss-agent/rss-agent.db"
}

func (c *Config) ProfileHash() string {
	data, _ := json.Marshal(c.Profile)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:16]
}

func (c *Config) ModelCandidates() []Model {
	if !isZeroModel(c.Models.Primary) || len(c.Models.Fallback) > 0 {
		models := []Model{}
		if !isZeroModel(c.Models.Primary) {
			models = append(models, c.Models.Primary)
		}
		models = append(models, c.Models.Fallback...)
		return models
	}
	if isZeroModel(c.Model) {
		return nil
	}
	return []Model{c.Model}
}

func (c *Config) ResolvedModel() (ResolvedModel, error) {
	models, err := c.ResolvedModels()
	if err != nil {
		return ResolvedModel{}, err
	}
	if len(models) == 0 {
		return ResolvedModel{}, errors.New("没有可用模型")
	}
	return models[0], nil
}

func (c *Config) ResolvedModels() ([]ResolvedModel, error) {
	var resolved []ResolvedModel
	for _, model := range c.ModelCandidates() {
		if isDisabled(model) {
			continue
		}
		model = applyModelDefaults(model)
		if model.Name == "" {
			continue
		}
		item, err := resolveModel(model)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, item)
	}
	if len(resolved) == 0 {
		return nil, errors.New("没有可用模型：请设置 models.primary.name 或 model.name")
	}
	if c.Budget.StopWhenFreeQuotaMissing && !hasFreeQuota(resolved) {
		return nil, errors.New("预算熔断：当前模型池没有配置 free_daily_tokens，请授权火山方舟免费资源或关闭 budget.stop_when_free_quota_missing")
	}
	return resolved, nil
}

func resolveModel(model Model) (ResolvedModel, error) {
	apiKey := model.APIKey
	if apiKey == "" && model.APIKeyEnv != "" {
		apiKey = os.Getenv(model.APIKeyEnv)
	}
	if apiKey == "" {
		return ResolvedModel{}, fmt.Errorf("缺少模型 API key：请设置 %s 的 api_key 或环境变量 %s", firstNonEmpty(model.Label, model.Name), model.APIKeyEnv)
	}
	if model.Name == "" {
		return ResolvedModel{}, errors.New("缺少模型名：请设置 model.name")
	}
	return ResolvedModel{
		Label:                    firstNonEmpty(model.Label, model.Name),
		Provider:                 normalizeProvider(model.Provider),
		BaseURL:                  model.BaseURL,
		APIKey:                   apiKey,
		Name:                     model.Name,
		Timeout:                  mustDuration(model.Timeout, 60*time.Second),
		Temperature:              model.Temperature,
		MaxTokens:                model.MaxTokens,
		InputPriceCNYPerMillion:  model.InputPriceCNYPerMillion,
		OutputPriceCNYPerMillion: model.OutputPriceCNYPerMillion,
		FreeDailyTokens:          model.FreeDailyTokens,
	}, nil
}

func (c *Config) WebhookURL() string {
	if c.Push.WebhookURL != "" {
		return c.Push.WebhookURL
	}
	if c.Push.WebhookURLEnv != "" {
		return os.Getenv(c.Push.WebhookURLEnv)
	}
	return ""
}

func (c *Config) EnabledFeeds() []Feed {
	feeds := make([]Feed, 0, len(c.Feeds))
	for _, feed := range c.Feeds {
		if !feed.Disabled {
			feeds = append(feeds, feed)
		}
	}
	return feeds
}

func mustDuration(raw string, fallback time.Duration) time.Duration {
	d, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return d
}

func applyModelDefaults(model Model) Model {
	if isZeroModel(model) {
		return model
	}
	provider := normalizeProvider(model.Provider)
	if provider == "" {
		provider = "openai"
	}
	model.Provider = provider
	if model.BaseURL == "" {
		model.BaseURL = defaultBaseURL(provider)
	}
	if model.APIKeyEnv == "" {
		model.APIKeyEnv = defaultAPIKeyEnv(provider)
	}
	if model.Timeout == "" {
		model.Timeout = "60s"
	}
	if model.Temperature == 0 {
		model.Temperature = 0.2
	}
	if model.MaxTokens == 0 {
		model.MaxTokens = 1200
	}
	return model
}

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func isSupportedProvider(provider string) bool {
	switch normalizeProvider(provider) {
	case "", "openai", "ark", "doubao", "deepseek", "gemini", "qwen":
		return true
	default:
		return false
	}
}

func defaultBaseURL(provider string) string {
	switch normalizeProvider(provider) {
	case "ark", "doubao":
		return "https://ark.cn-beijing.volces.com/api/v3"
	case "deepseek":
		return "https://api.deepseek.com"
	case "gemini":
		return "https://generativelanguage.googleapis.com/v1beta/openai/"
	case "qwen":
		return "https://dashscope.aliyuncs.com/compatible-mode/v1"
	default:
		return os.Getenv("OPENAI_BASE_URL")
	}
}

func defaultAPIKeyEnv(provider string) string {
	switch normalizeProvider(provider) {
	case "ark", "doubao":
		return "ARK_API_KEY"
	case "deepseek":
		return "DEEPSEEK_API_KEY"
	case "gemini":
		return "GEMINI_API_KEY"
	case "qwen":
		return "DASHSCOPE_API_KEY"
	default:
		return "OPENAI_API_KEY"
	}
}

func isZeroModel(model Model) bool {
	return model.Provider == "" &&
		model.BaseURL == "" &&
		model.APIKey == "" &&
		model.APIKeyEnv == "" &&
		model.Name == "" &&
		model.Label == ""
}

func isDisabled(model Model) bool {
	return model.Enabled != nil && !*model.Enabled
}

func hasFreeQuota(models []ResolvedModel) bool {
	for _, model := range models {
		if model.FreeDailyTokens > 0 {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func Sample() *Config {
	candidateLimit := 24
	cfg := &Config{
		Profile: Profile{
			Language: "zh-CN",
			Timezone: "Asia/Shanghai",
			Interests: []string{
				"AI Agent、Eino、Go 工程实践、LLM 应用架构",
				"能直接启发产品设计或开发效率的工具、论文和案例",
			},
			MustInclude:   []string{},
			PriorityTerms: []string{"Eino", "AI Agent", "Go", "LLM"},
			MutedFeeds:    []string{},
			MutedTags:     []string{},
			Exclude: []string{
				"纯融资新闻",
				"标题党营销稿",
			},
			Notes: "偏好可落地、有技术细节、信息密度高的内容。",
		},
		Models: ModelPool{
			Primary: Model{
				Label:                    "ark-deepseek-v3.2",
				Provider:                 "ark",
				BaseURL:                  "https://ark.cn-beijing.volces.com/api/v3",
				APIKeyEnv:                "ARK_API_KEY",
				Name:                     "${ARK_MODEL}",
				Timeout:                  "60s",
				Temperature:              0.2,
				MaxTokens:                1200,
				FreeDailyTokens:          2_000_000,
				InputPriceCNYPerMillion:  0,
				OutputPriceCNYPerMillion: 0,
			},
			Fallback: []Model{
				{
					Label:                    "deepseek-v4-flash",
					Provider:                 "deepseek",
					APIKeyEnv:                "DEEPSEEK_API_KEY",
					Name:                     "${DEEPSEEK_MODEL}",
					Timeout:                  "60s",
					Temperature:              0.2,
					MaxTokens:                1200,
					InputPriceCNYPerMillion:  0.95,
					OutputPriceCNYPerMillion: 1.9,
				},
			},
		},
		Settings: Settings{
			Interval:            "30m",
			HTTPTimeout:         "20s",
			LookbackHours:       72,
			MaxItemsPerFeed:     20,
			BatchSize:           8,
			MinScore:            7,
			MaxPushes:           8,
			MaxCandidatesPerRun: &candidateLimit,
			AnalysisCacheTTL:    "168h",
		},
		Database: Database{Path: ".rss-agent/rss-agent.db"},
		Budget: Budget{
			MonthlyCNY:               20,
			LLMMonthlyCNY:            5,
			XMonthlyCNY:              10,
			HardStopCNY:              19,
			StopWhenFreeQuotaMissing: false,
			WarnWhenUsedRatio:        0.8,
		},
		Push: Push{
			Console:       true,
			WebhookURLEnv: "RSS_AGENT_WEBHOOK_URL",
		},
		State: StateConfig{Path: ".rss-agent/state.json"},
		Feeds: []Feed{
			{
				Name: "CloudWeGo Blog",
				URL:  "https://www.cloudwego.io/feed.xml",
				Tags: []string{"go", "eino"},
			},
		},
	}
	cfg.ApplyDefaults()
	return cfg
}
