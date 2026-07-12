package app

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/compose"
	"github.com/jeffery/rss-agent/internal/agent"
	"github.com/jeffery/rss-agent/internal/config"
	"github.com/jeffery/rss-agent/internal/discovery"
	"github.com/jeffery/rss-agent/internal/rss"
	"github.com/jeffery/rss-agent/internal/store"
	"github.com/jeffery/rss-agent/internal/triage"
	xsource "github.com/jeffery/rss-agent/internal/x"
)

type RunOptions struct {
	DryRun      bool
	IncludeSeen bool
	ProfileID   string
	CollectOnly bool
}

type RunSummary struct {
	RunID       int64
	Fetched     int
	NotModified int
	Candidate   int
	Cached      int
	Analyzed    int
	Queued      int
	RateLimited int
	Pushed      int
	LLMCostCNY  float64
	Triage      triage.Stats
	Errors      []error
}

type runState struct {
	runID      int64
	cfg        *config.Config
	options    RunOptions
	db         *store.DB
	fetcher    *rss.Fetcher
	summary    RunSummary
	fetched    []rss.Item
	candidates []rss.Item
	initial    []rss.Item
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
	if !options.DryRun {
		state.runID, err = db.CreateAnalysisRun(ctx, options.ProfileID, "initial")
		if err != nil {
			return RunSummary{}, err
		}
		state.summary.RunID = state.runID
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

// AnalyzeSavedItem analyzes one existing digest item without pushing it to delivery channels.
func AnalyzeSavedItem(ctx context.Context, cfg *config.Config, profileID string, itemID string) (RunSummary, error) {
	db, err := store.Open(cfg.DatabasePath())
	if err != nil {
		return RunSummary{}, err
	}
	defer db.Close()

	item, err := db.ItemForProfile(ctx, profileID, itemID)
	if err != nil {
		return RunSummary{}, err
	}
	feedback, err := db.FeedbackFiltersForProfile(ctx, profileID)
	if err != nil {
		return RunSummary{}, err
	}
	if feedback.BlockedItemIDs[item.StableID()] || feedback.BlockedFeedURLs[item.FeedURL] {
		return RunSummary{}, fmt.Errorf("item %q is blocked by feedback", itemID)
	}
	fullText := rss.NewFetcher(cfg.HTTPTimeout()).EnrichFullText(ctx, []rss.Item{item}, cfg.Settings.FullTextMinChars, cfg.Settings.FullTextMaxChars)
	if len(fullText.Items) > 0 {
		item = fullText.Items[0]
		if err := db.UpsertItem(ctx, item); err != nil {
			return RunSummary{}, err
		}
	}

	summary := RunSummary{Candidate: 1}
	cached, uncached, err := splitCached(ctx, db, []rss.Item{item}, cfg.ProfileHash(), cfg.AnalysisCacheTTL())
	if err != nil {
		return summary, err
	}
	summary.Cached = len(cached)
	if len(uncached) == 0 {
		return summary, nil
	}
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
	for _, result := range fresh {
		if err := db.SaveAnalysis(ctx, result.Item, cfg.ProfileHash(), result.ModelLabel, result.ModelName, result.Decision); err != nil {
			return summary, err
		}
	}
	cost, err := recordUsageCosts(ctx, db, modelCfgs, usages)
	if err != nil {
		return summary, err
	}
	summary.LLMCostCNY = cost
	return summary, nil
}

// DrainAnalysisQueue processes persisted background work until no ready tasks remain.
func DrainAnalysisQueue(ctx context.Context, cfg *config.Config, profileID string) error {
	db, err := store.Open(cfg.DatabasePath())
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.RecoverAnalysisTasks(ctx, time.Now().Add(-10*time.Minute)); err != nil {
		return err
	}
	modelCfgs, err := cfg.ResolvedModels()
	if err != nil {
		return err
	}
	rssAgent, err := agent.NewPool(ctx, modelCfgs)
	if err != nil {
		return err
	}
	agent.ConfigureRateLimit(cfg.Settings.AnalysisRPM, cfg.Settings.AnalysisTPM)
	for {
		tasks, err := db.ClaimAnalysisTasks(ctx, profileID, cfg.Settings.BatchSize)
		if err != nil {
			return err
		}
		if len(tasks) == 0 {
			stats, statsErr := db.AnalysisQueueStats(ctx, profileID)
			if statsErr != nil {
				return statsErr
			}
			if stats.Pending+stats.Running+stats.Retrying+stats.RateLimited == 0 {
				return nil
			}
			timer := time.NewTimer(5 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
		items := make([]rss.Item, 0, len(tasks))
		for _, task := range tasks {
			item, itemErr := db.ItemForProfile(ctx, profileID, task.ItemID)
			if itemErr != nil {
				_ = db.FailAnalysisTasks(ctx, []int64{task.ID}, itemErr.Error())
				continue
			}
			items = append(items, item)
		}
		if len(items) == 0 {
			continue
		}
		results, usages, analyzeErr := rssAgent.AnalyzeWithUsage(ctx, cfg.Profile, items, cfg.Settings.BatchSize)
		taskByItem := map[string]store.AnalysisTask{}
		for _, task := range tasks {
			taskByItem[task.ItemID] = task
		}
		var completedIDs []int64
		for _, result := range results {
			if err := db.SaveAnalysis(ctx, result.Item, cfg.ProfileHash(), result.ModelLabel, result.ModelName, result.Decision); err != nil {
				return err
			}
			if task, ok := taskByItem[result.Item.StableID()]; ok {
				completedIDs = append(completedIDs, task.ID)
				delete(taskByItem, result.Item.StableID())
			}
		}
		if _, err := recordUsageCosts(ctx, db, modelCfgs, usages); err != nil {
			return err
		}
		if err := db.CompleteAnalysisTasks(ctx, completedIDs); err != nil {
			return err
		}
		var remaining []int64
		maxAttempts := 0
		for _, task := range taskByItem {
			remaining = append(remaining, task.ID)
			if task.Attempts > maxAttempts {
				maxAttempts = task.Attempts
			}
		}
		if len(remaining) > 0 {
			message := "模型未返回该条目的有效结果"
			if analyzeErr != nil {
				message = analyzeErr.Error()
			}
			if isRateLimitError(analyzeErr) {
				if err := db.RetryAnalysisTasks(ctx, remaining, message, time.Now().Add(rateLimitDelay(analyzeErr)), false); err != nil {
					return err
				}
			} else if maxAttempts >= 2 {
				if err := db.FailAnalysisTasks(ctx, remaining, message); err != nil {
					return err
				}
			} else {
				if err := db.RetryAnalysisTasks(ctx, remaining, message, time.Now().Add(time.Duration(maxAttempts+1)*15*time.Second), true); err != nil {
					return err
				}
			}
		}
	}
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
		{"analyze", compose.END},
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
	state.fetched = append([]rss.Item(nil), fetched.Items...)
	state.summary = RunSummary{
		RunID:       state.runID,
		Fetched:     len(fetched.Items),
		NotModified: fetched.NotModified,
		Errors:      fetched.Errs,
	}
	if searches := state.cfg.EnabledXSearches(); len(searches) > 0 {
		xFetched := xsource.New(state.cfg.X.BaseURL, state.cfg.XBearerToken(), &http.Client{Timeout: state.cfg.HTTPTimeout()}).FetchSearches(ctx, searches)
		state.fetched = append(state.fetched, xFetched.Items...)
		state.summary.Fetched += len(xFetched.Items)
		state.summary.Errors = append(state.summary.Errors, xFetched.Errs...)
	}
	if !state.options.DryRun {
		discovered := discovery.Fetch(ctx, &http.Client{Timeout: state.cfg.HTTPTimeout()}, state.cfg.Settings.MaxItemsPerFeed)
		state.fetched = append(state.fetched, discovered.Items...)
		state.summary.Fetched += len(discovered.Items)
		state.summary.Errors = append(state.summary.Errors, discovered.Errors...)
	}
	cutoff := retentionCutoff(time.Now(), state.cfg.Profile.Timezone, state.cfg.Settings.RetentionDays)
	kept := state.fetched[:0]
	for _, item := range state.fetched {
		if item.Time().IsZero() || !item.Time().Before(cutoff) {
			kept = append(kept, item)
		}
	}
	state.fetched = kept
	if !state.options.DryRun {
		if err := state.db.UpsertItemsForProfile(ctx, state.options.ProfileID, state.fetched); err != nil {
			return state, err
		}
		if _, err := state.db.CleanupOldItems(ctx, state.options.ProfileID, cutoff); err != nil {
			return state, err
		}
	}
	return state, nil
}

func retentionCutoff(now time.Time, timezone string, days int) time.Time {
	if days <= 0 {
		days = 2
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		location = time.Local
	}
	local := now.In(location)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	return start.AddDate(0, 0, -(days - 1))
}

func filterRunNode(ctx context.Context, state *runState) (*runState, error) {
	feedback, err := state.db.FeedbackFiltersForProfile(ctx, state.options.ProfileID)
	if err != nil {
		return state, err
	}
	settings := state.cfg.Settings
	unlimited := 0
	settings.MaxCandidatesPerRun = &unlimited
	filtered := triage.FilterWithFeedback(state.fetched, map[string]bool{}, triage.FeedbackRules{
		BlockedItemIDs:  feedback.BlockedItemIDs,
		BlockedFeedURLs: feedback.BlockedFeedURLs,
	}, state.cfg.Profile, settings, state.options.IncludeSeen, time.Now())
	state.candidates = filtered.Items
	state.initial = newestPerFeed(state.candidates, state.cfg.Settings.InitialItemsPerFeed)
	state.candidates = append([]rss.Item(nil), state.initial...)
	state.summary.Triage = filtered.Stats
	state.summary.Candidate = len(state.candidates)
	return state, nil
}

func newestPerFeed(items []rss.Item, limit int) []rss.Item {
	if limit <= 0 {
		limit = 10
	}
	groups := make(map[string][]rss.Item)
	for _, item := range items {
		groups[item.FeedURL] = append(groups[item.FeedURL], item)
	}
	var selected []rss.Item
	for _, group := range groups {
		sort.SliceStable(group, func(i, j int) bool { return group[i].Time().After(group[j].Time()) })
		if len(group) > limit {
			group = group[:limit]
		}
		selected = append(selected, group...)
	}
	sort.SliceStable(selected, func(i, j int) bool { return selected[i].Time().After(selected[j].Time()) })
	return selected
}

func enrichRunNode(ctx context.Context, state *runState) (*runState, error) {
	if len(state.initial) == 0 {
		return state, nil
	}
	fullText := state.fetcher.EnrichFullText(ctx, state.initial, state.cfg.Settings.FullTextMinChars, state.cfg.Settings.FullTextMaxChars)
	state.initial = fullText.Items
	byID := make(map[string]rss.Item, len(state.initial))
	for _, item := range state.initial {
		byID[item.StableID()] = item
	}
	for i, item := range state.candidates {
		if enriched, ok := byID[item.StableID()]; ok {
			state.candidates[i] = enriched
		}
	}
	state.summary.Errors = append(state.summary.Errors, fullText.Errs...)
	if !state.options.DryRun {
		for _, item := range state.initial {
			if err := state.db.UpsertItem(ctx, item); err != nil {
				return state, err
			}
		}
	}
	return state, nil
}

func analyzeRunNode(ctx context.Context, state *runState) (*runState, error) {
	if len(state.candidates) == 0 {
		if !state.options.DryRun {
			_ = state.db.SetAnalysisRunTotals(ctx, state.runID, 0, 0, "completed")
		}
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
		if !state.options.DryRun {
			_ = state.db.SetAnalysisRunTotals(ctx, state.runID, len(cached), len(cached), "completed")
		}
		return state, nil
	}
	initialIDs := make(map[string]bool, len(state.initial))
	for _, item := range state.initial {
		initialIDs[item.StableID()] = true
	}
	var foreground []rss.Item
	for _, item := range uncached {
		priority := 10
		if initialIDs[item.StableID()] {
			priority = 100
			foreground = append(foreground, item)
		}
		if !state.options.DryRun {
			if _, err := state.db.EnqueueAnalysis(ctx, state.runID, state.options.ProfileID, profileHash, state.cfg.Settings.AnalysisPromptVersion, item, priority); err != nil {
				return state, err
			}
		}
	}
	state.summary.Queued = len(uncached) - len(foreground)
	if !state.options.DryRun {
		if err := state.db.SetAnalysisRunTotals(ctx, state.runID, len(cached)+len(uncached), len(cached), "initial"); err != nil {
			return state, err
		}
	}
	if len(foreground) == 0 {
		if !state.options.DryRun {
			_ = state.db.SetAnalysisRunStatus(ctx, state.runID, "background")
		}
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
	initialTPM := state.cfg.Settings.AnalysisTPM
	if state.cfg.Settings.InitialTokenBudget > 0 && state.cfg.Settings.InitialTokenBudget < initialTPM {
		initialTPM = state.cfg.Settings.InitialTokenBudget
	}
	agent.ConfigureRateLimit(state.cfg.Settings.AnalysisRPM, initialTPM)
	var fresh []agent.Result
	var usages []agent.Usage
	for {
		fresh, usages, err = rssAgent.AnalyzeWithUsage(ctx, state.cfg.Profile, foreground, state.cfg.Settings.BatchSize)
		if err == nil || len(fresh) > 0 {
			break
		}
		if !isRateLimitError(err) {
			return state, err
		}
		state.summary.RateLimited = len(foreground)
		timer := time.NewTimer(rateLimitDelay(err))
		select {
		case <-ctx.Done():
			timer.Stop()
			return state, ctx.Err()
		case <-timer.C:
		}
	}
	state.summary.Analyzed = len(fresh)
	if !state.options.DryRun {
		for _, result := range fresh {
			if err := state.db.SaveAnalysis(ctx, result.Item, profileHash, result.ModelLabel, result.ModelName, result.Decision); err != nil {
				return state, err
			}
		}
		completedItems := make([]rss.Item, 0, len(fresh))
		for _, result := range fresh {
			completedItems = append(completedItems, result.Item)
		}
		if err := state.db.CompleteAnalysisItems(ctx, profileHash, completedItems); err != nil {
			return state, err
		}
		cost, err := recordUsageCosts(ctx, state.db, modelCfgs, usages)
		if err != nil {
			return state, err
		}
		state.summary.LLMCostCNY = cost
		_ = state.db.SetAnalysisRunStatus(ctx, state.runID, "background")
	}
	if err != nil {
		state.summary.Errors = append(state.summary.Errors, err)
	}
	state.decisions = append(state.decisions, fresh...)
	return state, nil
}

func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "429") || strings.Contains(text, "rate limit") || strings.Contains(text, "too many requests")
}

var retryAfterPattern = regexp.MustCompile(`(?i)retry-after[^0-9]*(\d+)`)

func rateLimitDelay(err error) time.Duration {
	if match := retryAfterPattern.FindStringSubmatch(err.Error()); len(match) == 2 {
		if seconds, parseErr := strconv.Atoi(match[1]); parseErr == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return time.Minute
}

func localDayStart(now time.Time, timezone string) time.Time {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		location = time.Local
	}
	local := now.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location).UTC()
}

func digestSlot(now time.Time, timezone string) string {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		location = time.Local
	}
	hour := now.In(location).Hour()
	if hour < 12 {
		return "morning"
	}
	if hour >= 18 {
		return "evening"
	}
	return "manual"
}

func Watch(ctx context.Context, cfg *config.Config, options RunOptions) error {
	interval := cfg.Interval()
	for {
		started := time.Now()
		runOptions := options
		runOptions.CollectOnly = !isDigestWindow(time.Now(), cfg.Profile.Timezone, cfg.Digest.Times)
		summary, err := RunOnce(ctx, cfg, runOptions)
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

func isDigestWindow(now time.Time, timezone string, times []string) bool {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		location = time.Local
	}
	local := now.In(location)
	for _, raw := range times {
		parsed, err := time.Parse("15:04", raw)
		if err == nil && local.Hour() == parsed.Hour() {
			return true
		}
	}
	return false
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
