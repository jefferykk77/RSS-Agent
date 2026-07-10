package app

import (
	"context"
	"fmt"
	"sort"
	"time"

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

func RunOnce(ctx context.Context, cfg *config.Config, options RunOptions) (RunSummary, error) {
	db, err := store.Open(cfg.DatabasePath())
	if err != nil {
		return RunSummary{}, err
	}
	defer db.Close()

	fetcher := rss.NewFetcher(cfg.HTTPTimeout())
	var feedStore rss.FeedStateStore
	if !options.DryRun {
		feedStore = db
	}
	fetched := fetcher.Fetch(ctx, cfg.EnabledFeeds(), cfg.Settings.MaxItemsPerFeed, feedStore)
	summary := RunSummary{
		Fetched:     len(fetched.Items),
		NotModified: fetched.NotModified,
		Errors:      fetched.Errs,
	}
	if !options.DryRun {
		for _, item := range fetched.Items {
			if err := db.UpsertItem(ctx, item); err != nil {
				return summary, err
			}
		}
	}

	seenIDs := map[string]bool{}
	if !options.IncludeSeen {
		seenIDs, err = db.SeenIDs(ctx)
		if err != nil {
			return summary, err
		}
	}

	filtered := triage.Filter(fetched.Items, seenIDs, cfg.Profile, cfg.Settings, options.IncludeSeen, time.Now())
	candidates := filtered.Items
	summary.Triage = filtered.Stats
	summary.Candidate = len(candidates)
	if len(candidates) == 0 {
		return summary, nil
	}

	profileHash := cfg.ProfileHash()
	cached, uncached, err := splitCached(ctx, db, candidates, profileHash, cfg.AnalysisCacheTTL())
	if err != nil {
		return summary, err
	}
	summary.Cached = len(cached)
	decisions := cached
	if len(uncached) > 0 {
		if err := checkBudget(ctx, db, cfg); err != nil {
			return summary, err
		}
		modelCfgs, err := cfg.ResolvedModels()
		if err != nil {
			return summary, err
		}
		rssAgent, err := agent.NewPool(ctx, modelCfgs)
		if err != nil {
			return summary, err
		}
		fresh, usages, err := rssAgent.AnalyzeWithUsage(ctx, cfg.Profile, uncached, cfg.Settings.BatchSize)
		if err != nil {
			return summary, err
		}
		summary.Analyzed = len(fresh)
		if !options.DryRun {
			for _, result := range fresh {
				if err := db.SaveAnalysis(ctx, result.Item, profileHash, result.ModelLabel, result.ModelName, result.Decision); err != nil {
					return summary, err
				}
			}
			cost, err := recordUsageCosts(ctx, db, modelCfgs, usages)
			if err != nil {
				return summary, err
			}
			summary.LLMCostCNY = cost
		}
		decisions = append(decisions, fresh...)
	}
	sortResults(decisions)

	selected := selectPushes(decisions, cfg.Settings.MinScore, cfg.Settings.MaxPushes)
	summary.Pushed = len(selected)
	if options.DryRun {
		fmt.Print(push.FormatMarkdown(selected))
		return summary, nil
	}

	pusher := &push.Pusher{
		Console:    cfg.Push.Console,
		WebhookURL: cfg.WebhookURL(),
	}
	if err := pusher.Push(ctx, selected); err != nil {
		for _, result := range selected {
			_ = db.RecordPush(ctx, result.Item.StableID(), "push", false, err.Error())
		}
		return summary, err
	}

	pushed := make(map[string]bool, len(selected))
	for _, result := range selected {
		pushed[result.Item.StableID()] = true
		if err := db.RecordPush(ctx, result.Item.StableID(), "push", true, ""); err != nil {
			return summary, err
		}
	}
	for _, item := range candidates {
		if err := db.MarkSeen(ctx, item, pushed[item.StableID()]); err != nil {
			return summary, err
		}
	}
	return summary, nil
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
	fmt.Printf("本地筛选：跳过 %d 条（重复 %d、已读 %d、过期 %d、静默 %d、排除 %d、未命中必须项 %d、候选限额 %d）。\n",
		summary.Triage.Skipped(),
		summary.Triage.Duplicate,
		summary.Triage.Seen,
		summary.Triage.Stale,
		summary.Triage.Muted,
		summary.Triage.Excluded,
		summary.Triage.MissingRequired,
		summary.Triage.Capped)
}
