package app

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/cloudwego/eino/compose"
	"github.com/jeffery/rss-agent/internal/agent"
	"github.com/jeffery/rss-agent/internal/config"
	"github.com/jeffery/rss-agent/internal/push"
	"github.com/jeffery/rss-agent/internal/rss"
	"github.com/jeffery/rss-agent/internal/store"
	"github.com/jeffery/rss-agent/internal/triage"
)

type RunOptions struct {
	DryRun      bool
	IncludeSeen bool
	ProfileID   string
}

type RunSummary struct {
	Fetched     int
	NotModified int
	Candidate   int
	Cached      int
	Analyzed    int
	Pushed      int
	LLMCostCNY  float64
	Triage      triage.Stats
	Errors      []error
}

type runState struct {
	cfg        *config.Config
	options    RunOptions
	db         *store.DB
	fetcher    *rss.Fetcher
	summary    RunSummary
	fetched    []rss.Item
	candidates []rss.Item
	decisions  []agent.Result
}

func RunOnce(ctx context.Context, cfg *config.Config, options RunOptions) (RunSummary, error) {
	db, err := store.Open(cfg.DatabasePath())
	if err != nil {
		return RunSummary{}, err
	}
	defer db.Close()

	state := &runState{
		cfg:     cfg,
		options: options,
		db:      db,
		fetcher: rss.NewFetcher(cfg.HTTPTimeout()),
	}
	flow, err := newRunGraph(ctx)
	if err != nil {
		return state.summary, err
	}
	output, err := flow.Invoke(ctx, state)
	if output == nil {
		output = state
	}
	return output.summary, err
}

func newRunGraph(ctx context.Context) (compose.Runnable[*runState, *runState], error) {
	graph := compose.NewGraph[*runState, *runState]()
	nodes := []struct {
		key string
		run func(context.Context, *runState) (*runState, error)
	}{
		{key: "fetch", run: fetchRunNode},
		{key: "filter", run: filterRunNode},
		{key: "enrich", run: enrichRunNode},
		{key: "analyze", run: analyzeRunNode},
		{key: "push", run: pushRunNode},
	}
	for _, node := range nodes {
		if err := graph.AddLambdaNode(node.key, compose.InvokableLambda[*runState, *runState](node.run)); err != nil {
			return nil, fmt.Errorf("添加 %s 节点失败：%w", node.key, err)
		}
	}
	edges := [][2]string{
		{compose.START, "fetch"},
		{"fetch", "filter"},
		{"filter", "enrich"},
		{"enrich", "analyze"},
		{"analyze", "push"},
		{"push", compose.END},
	}
	for _, edge := range edges {
		if err := graph.AddEdge(edge[0], edge[1]); err != nil {
			return nil, fmt.Errorf("添加 %s -> %s 边失败：%w", edge[0], edge[1], err)
		}
	}
	return graph.Compile(ctx, compose.WithGraphName("rss-agent"))
}

func fetchRunNode(ctx context.Context, state *runState) (*runState, error) {
	var feedStore rss.FeedStateStore
	if !state.options.DryRun {
		feedStore = state.db
	}
	fetched := state.fetcher.Fetch(ctx, state.cfg.EnabledFeeds(), state.cfg.Settings.MaxItemsPerFeed, feedStore)
	state.fetched = fetched.Items
	state.summary = RunSummary{
		Fetched:     len(fetched.Items),
		NotModified: fetched.NotModified,
		Errors:      fetched.Errs,
	}
	if !state.options.DryRun {
		for _, item := range state.fetched {
			if err := state.db.UpsertItem(ctx, item); err != nil {
				return state, err
			}
			if err := state.db.UpsertProfileItem(ctx, state.options.ProfileID, item); err != nil {
				return state, err
			}
		}
	}
	return state, nil
}

func filterRunNode(ctx context.Context, state *runState) (*runState, error) {
	seenIDs := map[string]bool{}
	if !state.options.IncludeSeen {
		var err error
		seenIDs, err = state.db.SeenIDsForProfile(ctx, state.options.ProfileID)
		if err != nil {
			return state, err
		}
	}
	feedback, err := state.db.FeedbackFiltersForProfile(ctx, state.options.ProfileID)
	if err != nil {
		return state, err
	}
	filtered := triage.FilterWithFeedback(state.fetched, seenIDs, triage.FeedbackRules{
		BlockedItemIDs:  feedback.BlockedItemIDs,
		BlockedFeedURLs: feedback.BlockedFeedURLs,
	}, state.cfg.Profile, state.cfg.Settings, state.options.IncludeSeen, time.Now())
	state.candidates = filtered.Items
	state.summary.Triage = filtered.Stats
	state.summary.Candidate = len(state.candidates)
	return state, nil
}

func enrichRunNode(ctx context.Context, state *runState) (*runState, error) {
	if len(state.candidates) == 0 {
		return state, nil
	}
	fullText := state.fetcher.EnrichFullText(ctx, state.candidates, state.cfg.Settings.FullTextMinChars, state.cfg.Settings.FullTextMaxChars)
	state.candidates = fullText.Items
	state.summary.Errors = append(state.summary.Errors, fullText.Errs...)
	if !state.options.DryRun {
		for _, item := range state.candidates {
			if err := state.db.UpsertItem(ctx, item); err != nil {
				return state, err
			}
		}
	}
	return state, nil
}

func analyzeRunNode(ctx context.Context, state *runState) (*runState, error) {
	if len(state.candidates) == 0 {
		return state, nil
	}
	profileHash := state.cfg.ProfileHash()
	cached, uncached, err := splitCached(ctx, state.db, state.candidates, profileHash, state.cfg.AnalysisCacheTTL())
	if err != nil {
		return state, err
	}
	state.summary.Cached = len(cached)
	state.decisions = cached
	if len(uncached) == 0 {
		return state, nil
	}
	if err := checkBudget(ctx, state.db, state.cfg); err != nil {
		return state, err
	}
	modelCfgs, err := state.cfg.ResolvedModels()
	if err != nil {
		return state, err
	}
	rssAgent, err := agent.NewPool(ctx, modelCfgs)
	if err != nil {
		return state, err
	}
	fresh, usages, err := rssAgent.AnalyzeWithUsage(ctx, state.cfg.Profile, uncached, state.cfg.Settings.BatchSize)
	if err != nil {
		return state, err
	}
	state.summary.Analyzed = len(fresh)
	if !state.options.DryRun {
		for _, result := range fresh {
			if err := state.db.SaveAnalysis(ctx, result.Item, profileHash, result.ModelLabel, result.ModelName, result.Decision); err != nil {
				return state, err
			}
		}
		cost, err := recordUsageCosts(ctx, state.db, modelCfgs, usages)
		if err != nil {
			return state, err
		}
		state.summary.LLMCostCNY = cost
	}
	state.decisions = append(state.decisions, fresh...)
	return state, nil
}

func pushRunNode(ctx context.Context, state *runState) (*runState, error) {
	sortResults(state.decisions)
	selected := selectPushes(state.decisions, state.cfg.Settings.MinScore, state.cfg.Settings.MaxPushes)
	if state.options.DryRun {
		state.summary.Pushed = len(selected)
		fmt.Print(push.FormatMarkdown(selected))
		return state, nil
	}

	pusher := &push.Pusher{
		Console:            state.cfg.Push.Console,
		WebhookURL:         state.cfg.WebhookURL(),
		FeishuWebhookURL:   state.cfg.FeishuWebhookURL(),
		DingTalkWebhookURL: state.cfg.DingTalkWebhookURL(),
		TelegramBotToken:   state.cfg.TelegramBotToken(),
		TelegramChatID:     state.cfg.TelegramChatID(),
		Email: push.EmailConfig{
			SMTPHost: state.cfg.Push.Email.SMTPHost,
			SMTPPort: state.cfg.EmailSMTPPort(),
			Username: state.cfg.EmailUsername(),
			Password: state.cfg.EmailPassword(),
			From:     state.cfg.Push.Email.From,
			To:       state.cfg.Push.Email.To,
			Subject:  state.cfg.Push.Email.Subject,
			StartTLS: state.cfg.EmailStartTLS(),
		},
	}
	deliveries, pushErr := pusher.Push(ctx, selected)
	pushed := make(map[string]bool, len(selected))
	for _, delivery := range deliveries {
		for _, result := range selected {
			errText := ""
			if delivery.Err != nil {
				errText = delivery.Err.Error()
			}
			if err := state.db.RecordPush(ctx, result.Item.StableID(), delivery.Channel, delivery.Err == nil, errText); err != nil {
				return state, err
			}
			if delivery.Err == nil {
				pushed[result.Item.StableID()] = true
			}
		}
	}
	if len(pushed) > 0 {
		state.summary.Pushed = len(selected)
	}
	for _, item := range state.candidates {
		if err := state.db.MarkSeenForProfile(ctx, state.options.ProfileID, item, pushed[item.StableID()]); err != nil {
			return state, err
		}
	}
	if pushErr != nil {
		return state, pushErr
	}
	return state, nil
}

func Watch(ctx context.Context, cfg *config.Config, options RunOptions) error {
	interval := cfg.Interval()
	for {
		started := time.Now()
		summary, err := RunOnce(ctx, cfg, options)
		printSummary(summary, err)
		if err != nil {
			return err
		}

		wait := interval - time.Since(started)
		if wait < time.Second {
			wait = interval
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func selectPushes(results []agent.Result, minScore int, maxPushes int) []agent.Result {
	var selected []agent.Result
	for _, result := range results {
		if !result.Decision.ShouldPush || result.Decision.Score < minScore {
			continue
		}
		selected = append(selected, result)
		if maxPushes > 0 && len(selected) >= maxPushes {
			break
		}
	}
	return selected
}

func splitCached(ctx context.Context, db *store.DB, candidates []rss.Item, profileHash string, ttl time.Duration) ([]agent.Result, []rss.Item, error) {
	var cached []agent.Result
	var uncached []rss.Item
	for _, item := range candidates {
		result, ok, err := db.CachedAnalysis(ctx, item, profileHash, ttl)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			cached = append(cached, result)
			continue
		}
		uncached = append(uncached, item)
	}
	return cached, uncached, nil
}

func sortResults(results []agent.Result) {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Decision.Score == results[j].Decision.Score {
			return results[i].Item.Time().After(results[j].Item.Time())
		}
		return results[i].Decision.Score > results[j].Decision.Score
	})
}

func checkBudget(ctx context.Context, db *store.DB, cfg *config.Config) error {
	since := monthStart(time.Now())
	total, err := db.TotalCostSince(ctx, since)
	if err != nil {
		return err
	}
	if cfg.Budget.HardStopCNY > 0 && total >= cfg.Budget.HardStopCNY {
		return fmt.Errorf("预算熔断：本月总成本 %.2f 元已达到 hard_stop_cny %.2f 元", total, cfg.Budget.HardStopCNY)
	}
	llm, err := db.CostSince(ctx, "llm", since)
	if err != nil {
		return err
	}
	if cfg.Budget.LLMMonthlyCNY > 0 && llm >= cfg.Budget.LLMMonthlyCNY {
		return fmt.Errorf("预算熔断：本月 LLM 成本 %.2f 元已达到 llm_monthly_cny %.2f 元", llm, cfg.Budget.LLMMonthlyCNY)
	}
	return nil
}

func recordUsageCosts(ctx context.Context, db *store.DB, modelCfgs []config.ResolvedModel, usages []agent.Usage) (float64, error) {
	models := make(map[string]config.ResolvedModel, len(modelCfgs))
	for _, model := range modelCfgs {
		models[model.Provider+"|"+model.Name] = model
	}
	var total float64
	for _, usage := range usages {
		model := models[usage.Provider+"|"+usage.Model]
		usedToday, err := db.TokensSince(ctx, usage.Provider, usage.Model, dayStart(time.Now()))
		if err != nil {
			return total, err
		}
		cost := estimateCostCNY(usage, model, usedToday)
		total += cost
		if err := db.RecordCostEvent(ctx, store.CostEvent{
			Scope:        "llm",
			Provider:     usage.Provider,
			Model:        usage.Model,
			ModelLabel:   usage.ModelLabel,
			Kind:         "analysis",
			InputTokens:  usage.InputTokens,
			OutputTokens: usage.OutputTokens,
			CostCNY:      cost,
		}); err != nil {
			return total, err
		}
	}
	return total, nil
}

func estimateCostCNY(usage agent.Usage, model config.ResolvedModel, usedTokensToday int) float64 {
	inputCost := float64(usage.InputTokens) / 1_000_000 * model.InputPriceCNYPerMillion
	outputCost := float64(usage.OutputTokens) / 1_000_000 * model.OutputPriceCNYPerMillion
	base := inputCost + outputCost
	totalTokens := usage.InputTokens + usage.OutputTokens
	if base == 0 || totalTokens == 0 || model.FreeDailyTokens <= 0 {
		return base
	}
	freeRemaining := model.FreeDailyTokens - usedTokensToday
	if freeRemaining >= totalTokens {
		return 0
	}
	if freeRemaining <= 0 {
		return base
	}
	chargedRatio := float64(totalTokens-freeRemaining) / float64(totalTokens)
	return base * chargedRatio
}

func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

func dayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func printSummary(summary RunSummary, runErr error) {
	if runErr != nil {
		fmt.Printf("RSS Agent 运行失败：%v\n", runErr)
	}
	for _, err := range summary.Errors {
		fmt.Printf("抓取警告：%v\n", err)
	}
	fmt.Printf("RSS Agent：抓取 %d 条，候选 %d 条，分析 %d 条，推送 %d 条。\n",
		summary.Fetched, summary.Candidate, summary.Analyzed, summary.Pushed)
	fmt.Printf("本地筛选：跳过 %d 条（重复 %d、已读 %d、过期 %d、静默 %d、反馈屏蔽 %d、排除 %d、未命中必须项 %d、候选限额 %d）。\n",
		summary.Triage.Skipped(),
		summary.Triage.Duplicate,
		summary.Triage.Seen,
		summary.Triage.Stale,
		summary.Triage.Muted,
		summary.Triage.FeedbackBlocked,
		summary.Triage.Excluded,
		summary.Triage.MissingRequired,
		summary.Triage.Capped)
}
