package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const DefaultProfileName = "default"

type Config struct {
	Profile  Profile                  `yaml:"profile"`
	Model    Model                    `yaml:"model,omitempty"`
	Models   ModelPool                `yaml:"models,omitempty"`
	Settings Settings                 `yaml:"settings"`
	Database Database                 `yaml:"database"`
	Budget   Budget                   `yaml:"budget"`
	Push     Push                     `yaml:"push"`
	Feeds    []Feed                   `yaml:"feeds"`
	X        XConfig                  `yaml:"x,omitempty"`
	Digest   DigestConfig             `yaml:"digest,omitempty"`
	State    StateConfig              `yaml:"state,omitempty"`
	Profiles map[string]ProfileConfig `yaml:"profiles,omitempty"`
}

type ProfileConfig struct {
	Profile  Profile   `yaml:"profile"`
	Model    Model     `yaml:"model,omitempty"`
	Models   ModelPool `yaml:"models,omitempty"`
	Settings Settings  `yaml:"settings,omitempty"`
	Budget   Budget    `yaml:"budget,omitempty"`
	Push     Push      `yaml:"push,omitempty"`
	Feeds    []Feed    `yaml:"feeds,omitempty"`
	X        XConfig   `yaml:"x,omitempty"`
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
	Interval              string `yaml:"interval"`
	HTTPTimeout           string `yaml:"http_timeout"`
	LookbackHours         int    `yaml:"lookback_hours"`
	MaxItemsPerFeed       int    `yaml:"max_items_per_feed"`
	BatchSize             int    `yaml:"batch_size"`
	MinScore              int    `yaml:"min_score"`
	MaxPushes             int    `yaml:"max_pushes"`
	MaxCandidatesPerRun   *int   `yaml:"max_candidates_per_run"`
	AnalysisCacheTTL      string `yaml:"analysis_cache_ttl"`
	AnalysisPromptVersion string `yaml:"analysis_prompt_version,omitempty"`
	FullTextMinChars      int    `yaml:"full_text_min_chars"`
	FullTextMaxChars      int    `yaml:"full_text_max_chars"`
	InitialItemsPerFeed   int    `yaml:"initial_items_per_feed,omitempty"`
	AnalysisRPM           int    `yaml:"analysis_rpm,omitempty"`
	AnalysisTPM           int    `yaml:"analysis_tpm,omitempty"`
	InitialTokenBudget    int    `yaml:"initial_token_budget,omitempty"`
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
	Console       bool         `yaml:"console"`
	WebhookURL    string       `yaml:"webhook_url"`
	WebhookURLEnv string       `yaml:"webhook_url_env"`
	Feishu        WebhookPush  `yaml:"feishu,omitempty"`
	DingTalk      WebhookPush  `yaml:"dingtalk,omitempty"`
	Telegram      TelegramPush `yaml:"telegram,omitempty"`
	Email         EmailPush    `yaml:"email,omitempty"`
}

type DigestConfig struct {
	Times       []string `yaml:"times,omitempty"`
	DailyLimit  int      `yaml:"daily_limit,omitempty"`
	PerRunLimit int      `yaml:"per_run_limit,omitempty"`
}

type WebhookPush struct {
	WebhookURL    string `yaml:"webhook_url"`
	WebhookURLEnv string `yaml:"webhook_url_env"`
}

type TelegramPush struct {
	BotToken    string `yaml:"bot_token"`
	BotTokenEnv string `yaml:"bot_token_env"`
	ChatID      string `yaml:"chat_id"`
	ChatIDEnv   string `yaml:"chat_id_env"`
}

type EmailPush struct {
	SMTPHost    string   `yaml:"smtp_host"`
	SMTPPort    int      `yaml:"smtp_port"`
	Username    string   `yaml:"username"`
	UsernameEnv string   `yaml:"username_env"`
	Password    string   `yaml:"password"`
	PasswordEnv string   `yaml:"password_env"`
	From        string   `yaml:"from"`
	To          []string `yaml:"to"`
	Subject     string   `yaml:"subject"`
	StartTLS    *bool    `yaml:"start_tls,omitempty"`
}

type Feed struct {
	Name     string   `yaml:"name"`
	URL      string   `yaml:"url"`
	Tags     []string `yaml:"tags"`
	Disabled bool     `yaml:"disabled"`
}

type XConfig struct {
	BaseURL        string    `yaml:"base_url,omitempty"`
	BearerToken    string    `yaml:"bearer_token,omitempty"`
	BearerTokenEnv string    `yaml:"bearer_token_env,omitempty"`
	Searches       []XSearch `yaml:"searches,omitempty"`
}

type XSearch struct {
	Name       string   `yaml:"name"`
	Query      string   `yaml:"query"`
	Tags       []string `yaml:"tags,omitempty"`
	MaxResults int      `yaml:"max_results,omitempty"`
	Disabled   bool     `yaml:"disabled,omitempty"`
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
		c.Settings.Interval = "1h"
	}
	if len(c.Digest.Times) == 0 {
		c.Digest.Times = []string{"08:00", "20:00"}
	}
	if c.Digest.DailyLimit == 0 {
		c.Digest.DailyLimit = 12
	}
	if c.Digest.PerRunLimit == 0 {
		c.Digest.PerRunLimit = 6
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
		c.Settings.AnalysisCacheTTL = "24h"
	}
	if c.Settings.AnalysisPromptVersion == "" {
		c.Settings.AnalysisPromptVersion = "ai-frontier-v2"
	}
	if c.Settings.FullTextMinChars == 0 {
		c.Settings.FullTextMinChars = 600
	}
	if c.Settings.FullTextMaxChars == 0 {
		c.Settings.FullTextMaxChars = 8000
	}
	if c.Settings.InitialItemsPerFeed == 0 {
		c.Settings.InitialItemsPerFeed = 10
	}
	if c.Settings.AnalysisRPM == 0 {
		c.Settings.AnalysisRPM = 400
	}
	if c.Settings.AnalysisTPM == 0 {
		c.Settings.AnalysisTPM = 800000
	}
	if c.Settings.InitialTokenBudget == 0 {
		c.Settings.InitialTokenBudget = 700000
	}
	if len(c.X.Searches) > 0 {
		if c.X.BaseURL == "" {
			c.X.BaseURL = "https://api.x.com/2"
		}
		if c.X.BearerTokenEnv == "" {
			c.X.BearerTokenEnv = "X_BEARER_TOKEN"
		}
		for i := range c.X.Searches {
			if c.X.Searches[i].MaxResults == 0 {
				c.X.Searches[i].MaxResults = 20
			}
		}
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
	if c.Push.Email.StartTLS == nil {
		enabled := true
		c.Push.Email.StartTLS = &enabled
	}
}

func (c *Config) Validate() error {
	if err := c.validateSingle(); err != nil {
		return err
	}
	for name := range c.Profiles {
		name = strings.TrimSpace(name)
		if name == "" || name == DefaultProfileName {
			return fmt.Errorf("profiles 中不能使用保留名称 %q", name)
		}
		if _, err := c.ResolveProfile(name); err != nil {
			return fmt.Errorf("profiles.%s 无效：%w", name, err)
		}
	}
	return nil
}

func (c *Config) validateSingle() error {
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
	for i, search := range c.X.Searches {
		if strings.TrimSpace(search.Name) == "" {
			return fmt.Errorf("x.searches[%d].name cannot be empty", i)
		}
		if strings.TrimSpace(search.Query) == "" {
			return fmt.Errorf("x.searches[%d].query cannot be empty", i)
		}
		if search.MaxResults < 10 || search.MaxResults > 100 {
			return fmt.Errorf("x.searches[%d].max_results must be between 10 and 100", i)
		}
	}
	if c.Settings.MaxCandidatesPerRun != nil && *c.Settings.MaxCandidatesPerRun < 0 {
		return errors.New("settings.max_candidates_per_run cannot be negative")
	}
	if c.Settings.FullTextMinChars < 0 || c.Settings.FullTextMaxChars < 0 {
		return errors.New("settings.full_text_min_chars and settings.full_text_max_chars cannot be negative")
	}
	if c.Settings.FullTextMinChars > c.Settings.FullTextMaxChars {
		return errors.New("settings.full_text_max_chars must be greater than or equal to full_text_min_chars")
	}
	if c.Digest.DailyLimit < 1 || c.Digest.DailyLimit > 50 {
		return errors.New("digest.daily_limit must be between 1 and 50")
	}
	if c.Digest.PerRunLimit < 1 || c.Digest.PerRunLimit > c.Digest.DailyLimit {
		return errors.New("digest.per_run_limit must be positive and not exceed daily_limit")
	}
	for _, digestTime := range c.Digest.Times {
		if _, err := time.Parse("15:04", digestTime); err != nil {
			return fmt.Errorf("invalid digest time %q; use HH:MM", digestTime)
		}
	}
	if err := c.validatePush(); err != nil {
		return err
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

func (c *Config) validatePush() error {
	telegramToken := c.TelegramBotToken()
	telegramChatID := c.TelegramChatID()
	if (telegramToken == "") != (telegramChatID == "") {
		return errors.New("push.telegram requires both bot_token and chat_id")
	}

	email := c.Push.Email
	if email.SMTPHost == "" {
		if len(email.To) > 0 || email.Username != "" || email.Password != "" || email.From != "" {
			return errors.New("push.email.smtp_host is required when email delivery is configured")
		}
		return nil
	}
	if len(email.To) == 0 {
		return errors.New("push.email.to is required when email delivery is configured")
	}
	return nil
}

func (c *Config) ProfileNames() []string {
	names := make([]string, 0, len(c.Profiles)+1)
	names = append(names, DefaultProfileName)
	for name := range c.Profiles {
		name = strings.TrimSpace(name)
		if name != "" && name != DefaultProfileName {
			names = append(names, name)
		}
	}
	sort.Strings(names[1:])
	return names
}

func (c *Config) ResolveProfile(name string) (*Config, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = DefaultProfileName
	}
	resolved := copyConfig(c)
	resolved.Profiles = nil
	if name != DefaultProfileName {
		profile, ok := c.Profiles[name]
		if !ok {
			return nil, fmt.Errorf("未找到 profile %q；可用 profile：%s", name, strings.Join(c.ProfileNames(), ", "))
		}
		resolved.Profile = profile.Profile
		resolved.Settings = profile.Settings
		resolved.Budget = profile.Budget
		resolved.Push = profile.Push
		resolved.Feeds = append([]Feed(nil), profile.Feeds...)
		if hasProfileX(profile) {
			resolved.X = copyXConfig(profile.X)
		}
		if hasProfileModels(profile) {
			resolved.Model = profile.Model
			resolved.Models = copyModelPool(profile.Models)
		}
	}
	resolved.ApplyDefaults()
	if err := resolved.validateSingle(); err != nil {
		return nil, err
	}
	return &resolved, nil
}

func copyConfig(c *Config) Config {
	clone := *c
	clone.Feeds = append([]Feed(nil), c.Feeds...)
	clone.Models = copyModelPool(c.Models)
	clone.X = copyXConfig(c.X)
	return clone
}

func copyModelPool(pool ModelPool) ModelPool {
	pool.Fallback = append([]Model(nil), pool.Fallback...)
	return pool
}

func hasProfileModels(profile ProfileConfig) bool {
	return !isZeroModel(profile.Model) || !isZeroModel(profile.Models.Primary) || len(profile.Models.Fallback) > 0
}

func hasProfileX(profile ProfileConfig) bool {
	return profile.X.BaseURL != "" || profile.X.BearerToken != "" || profile.X.BearerTokenEnv != "" || len(profile.X.Searches) > 0
}

func copyXConfig(source XConfig) XConfig {
	source.Searches = append([]XSearch(nil), source.Searches...)
	for i := range source.Searches {
		source.Searches[i].Tags = append([]string(nil), source.Searches[i].Tags...)
	}
	return source
}

func (c *Config) Interval() time.Duration {
	return mustDuration(c.Settings.Interval, 30*time.Minute)
}

func (c *Config) HTTPTimeout() time.Duration {
	return mustDuration(c.Settings.HTTPTimeout, 20*time.Second)
}

func (c *Config) AnalysisCacheTTL() time.Duration {
	return mustDuration(c.Settings.AnalysisCacheTTL, 24*time.Hour)
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
	type modelIdentity struct {
		Label    string `json:"label"`
		Provider string `json:"provider"`
		BaseURL  string `json:"base_url"`
		Name     string `json:"name"`
	}
	identities := make([]modelIdentity, 0, len(c.ModelCandidates()))
	for _, model := range c.ModelCandidates() {
		if isDisabled(model) {
			continue
		}
		model = applyModelDefaults(model)
		identities = append(identities, modelIdentity{Label: model.Label, Provider: model.Provider, BaseURL: model.BaseURL, Name: model.Name})
	}
	payload := struct {
		Profile       Profile         `json:"profile"`
		Models        []modelIdentity `json:"models"`
		PromptVersion string          `json:"prompt_version"`
	}{Profile: c.Profile, Models: identities, PromptVersion: c.Settings.AnalysisPromptVersion}
	data, _ := json.Marshal(payload)
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
	return resolveValue(c.Push.WebhookURL, c.Push.WebhookURLEnv)
}

func (c *Config) FeishuWebhookURL() string {
	return resolveValue(c.Push.Feishu.WebhookURL, c.Push.Feishu.WebhookURLEnv)
}

func (c *Config) DingTalkWebhookURL() string {
	return resolveValue(c.Push.DingTalk.WebhookURL, c.Push.DingTalk.WebhookURLEnv)
}

func (c *Config) TelegramBotToken() string {
	return resolveValue(c.Push.Telegram.BotToken, c.Push.Telegram.BotTokenEnv)
}

func (c *Config) TelegramChatID() string {
	return resolveValue(c.Push.Telegram.ChatID, c.Push.Telegram.ChatIDEnv)
}

func (c *Config) EmailUsername() string {
	return resolveValue(c.Push.Email.Username, c.Push.Email.UsernameEnv)
}

func (c *Config) EmailPassword() string {
	return resolveValue(c.Push.Email.Password, c.Push.Email.PasswordEnv)
}

func (c *Config) EmailSMTPPort() int {
	if c.Push.Email.SMTPPort > 0 {
		return c.Push.Email.SMTPPort
	}
	return 587
}

func (c *Config) EmailStartTLS() bool {
	if c.Push.Email.StartTLS == nil {
		return true
	}
	return *c.Push.Email.StartTLS
}

func resolveValue(value string, envName string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv(strings.TrimSpace(envName)))
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

func (c *Config) EnabledXSearches() []XSearch {
	searches := make([]XSearch, 0, len(c.X.Searches))
	for _, search := range c.X.Searches {
		if !search.Disabled {
			searches = append(searches, search)
		}
	}
	return searches
}

func (c *Config) XBearerToken() string {
	return resolveValue(c.X.BearerToken, c.X.BearerTokenEnv)
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
	startTLS := true
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
			FullTextMinChars:    600,
			FullTextMaxChars:    8000,
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
			Feishu: WebhookPush{
				WebhookURLEnv: "FEISHU_WEBHOOK_URL",
			},
			DingTalk: WebhookPush{
				WebhookURLEnv: "DINGTALK_WEBHOOK_URL",
			},
			Telegram: TelegramPush{
				BotTokenEnv: "TELEGRAM_BOT_TOKEN",
				ChatIDEnv:   "TELEGRAM_CHAT_ID",
			},
			Email: EmailPush{
				SMTPPort:    587,
				UsernameEnv: "RSS_AGENT_SMTP_USERNAME",
				PasswordEnv: "RSS_AGENT_SMTP_PASSWORD",
				Subject:     "RSS Agent Digest",
				StartTLS:    &startTLS,
			},
		},
		State: StateConfig{Path: ".rss-agent/state.json"},
		Profiles: map[string]ProfileConfig{
			"product": {
				Profile: Profile{
					Language:      "zh-CN",
					Timezone:      "Asia/Shanghai",
					Interests:     []string{"AI 产品策略、开发者工具、产品增长与竞品动态"},
					MustInclude:   []string{},
					Exclude:       []string{"纯融资新闻", "标题党营销稿"},
					PriorityTerms: []string{"AI", "Agent", "product", "developer tools"},
					MutedFeeds:    []string{},
					MutedTags:     []string{},
					Notes:         "偏好产品方向、用户价值和可验证的市场信号。",
				},
				Push: Push{Console: true, WebhookURLEnv: "RSS_AGENT_WEBHOOK_URL"},
				Feeds: []Feed{
					{Name: "Go Blog", URL: "https://go.dev/blog/feed.atom", Tags: []string{"product", "developer-tools"}},
				},
			},
		},
		Feeds: []Feed{
			{
				Name: "Go Blog",
				URL:  "https://go.dev/blog/feed.atom",
				Tags: []string{"go", "eino"},
			},
		},
	}
	cfg.ApplyDefaults()
	return cfg
}
