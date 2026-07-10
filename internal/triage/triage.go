package triage

import (
	"sort"
	"strings"
	"time"

	"github.com/jeffery/rss-agent/internal/config"
	"github.com/jeffery/rss-agent/internal/rss"
)

type Stats struct {
	Duplicate       int
	Seen            int
	Stale           int
	Muted           int
	FeedbackBlocked int
	Excluded        int
	MissingRequired int
	Capped          int
}

func (s Stats) Skipped() int {
	return s.Duplicate + s.Seen + s.Stale + s.Muted + s.FeedbackBlocked + s.Excluded + s.MissingRequired + s.Capped
}

type Result struct {
	Items []rss.Item
	Stats Stats
}

type FeedbackRules struct {
	BlockedItemIDs  map[string]bool
	BlockedFeedURLs map[string]bool
}

type scoredItem struct {
	item  rss.Item
	score int
}

// Filter removes deterministic non-candidates before the LLM sees them.
func Filter(items []rss.Item, seenIDs map[string]bool, profile config.Profile, settings config.Settings, includeSeen bool, now time.Time) Result {
	return FilterWithFeedback(items, seenIDs, FeedbackRules{}, profile, settings, includeSeen, now)
}

// FilterWithFeedback removes deterministic non-candidates and feedback-blocked content before the LLM sees it.
func FilterWithFeedback(items []rss.Item, seenIDs map[string]bool, feedback FeedbackRules, profile config.Profile, settings config.Settings, includeSeen bool, now time.Time) Result {
	if now.IsZero() {
		now = time.Now()
	}

	lookback := time.Duration(settings.LookbackHours) * time.Hour
	cutoff := now.Add(-lookback)
	seenThisRun := make(map[string]bool, len(items))
	candidates := make([]scoredItem, 0, len(items))
	result := Result{}

	for _, item := range items {
		id := item.StableID()
		if seenThisRun[id] {
			result.Stats.Duplicate++
			continue
		}
		seenThisRun[id] = true
		if feedbackBlocked(item, feedback) {
			result.Stats.FeedbackBlocked++
			continue
		}
		if !includeSeen && seenIDs[id] {
			result.Stats.Seen++
			continue
		}
		if !item.Time().IsZero() && item.Time().Before(cutoff) {
			result.Stats.Stale++
			continue
		}
		if muted(item, profile) {
			result.Stats.Muted++
			continue
		}
		if containsAny(itemText(item), profile.Exclude) {
			result.Stats.Excluded++
			continue
		}
		if required := normalizeTerms(profile.MustInclude); len(required) > 0 && !containsAny(itemText(item), required) {
			result.Stats.MissingRequired++
			continue
		}

		item.LocalScore, item.LocalReasons = score(item, profile.PriorityTerms)
		candidates = append(candidates, scoredItem{item: item, score: item.LocalScore})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].item.Time().After(candidates[j].item.Time())
		}
		return candidates[i].score > candidates[j].score
	})

	limit := settings.CandidateLimit()
	if limit > 0 && len(candidates) > limit {
		result.Stats.Capped = len(candidates) - limit
		candidates = candidates[:limit]
	}
	result.Items = make([]rss.Item, 0, len(candidates))
	for _, candidate := range candidates {
		result.Items = append(result.Items, candidate.item)
	}
	return result
}

func feedbackBlocked(item rss.Item, feedback FeedbackRules) bool {
	if feedback.BlockedItemIDs[item.StableID()] {
		return true
	}
	return feedback.BlockedFeedURLs[item.FeedURL]
}

func score(item rss.Item, terms []string) (int, []string) {
	title := normalize(item.Title)
	metadata := normalize(strings.Join(append(append([]string{}, item.FeedTags...), item.Categories...), " "))
	body := normalize(strings.Join([]string{item.Summary, item.Content}, " "))

	score := 0
	reasons := make([]string, 0, len(terms))
	for _, term := range normalizeTerms(terms) {
		switch {
		case containsTerm(title, term):
			score += 5
			reasons = append(reasons, "priority term: "+term+" (title)")
		case containsTerm(metadata, term):
			score += 3
			reasons = append(reasons, "priority term: "+term+" (tag)")
		case containsTerm(body, term):
			score++
			reasons = append(reasons, "priority term: "+term+" (content)")
		}
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "recent item")
	}
	return score, reasons
}

func muted(item rss.Item, profile config.Profile) bool {
	if matchesExact(profile.MutedFeeds, item.FeedName) || matchesExact(profile.MutedFeeds, item.FeedURL) {
		return true
	}
	for _, tag := range append(append([]string{}, item.FeedTags...), item.Categories...) {
		if matchesExact(profile.MutedTags, tag) {
			return true
		}
	}
	return false
}

func itemText(item rss.Item) string {
	return normalize(strings.Join([]string{
		item.FeedName,
		item.FeedURL,
		strings.Join(item.FeedTags, " "),
		item.Title,
		item.Summary,
		item.Content,
		strings.Join(item.Categories, " "),
	}, " "))
}

func containsAny(text string, terms []string) bool {
	for _, term := range normalizeTerms(terms) {
		if containsTerm(text, term) {
			return true
		}
	}
	return false
}

func containsTerm(text string, term string) bool {
	if !isASCIIWord(term) {
		return strings.Contains(text, term)
	}
	for offset := 0; ; {
		index := strings.Index(text[offset:], term)
		if index < 0 {
			return false
		}
		start := offset + index
		end := start + len(term)
		if (start == 0 || !isASCIIWordByte(text[start-1])) && (end == len(text) || !isASCIIWordByte(text[end])) {
			return true
		}
		offset = end
	}
}

func isASCIIWord(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if !isASCIIWordByte(value[i]) {
			return false
		}
	}
	return true
}

func isASCIIWordByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= '0' && value <= '9' || value == '_'
}

func matchesExact(terms []string, value string) bool {
	value = normalize(value)
	for _, term := range normalizeTerms(terms) {
		if term == value {
			return true
		}
	}
	return false
}

func normalizeTerms(terms []string) []string {
	seen := make(map[string]bool, len(terms))
	result := make([]string, 0, len(terms))
	for _, term := range terms {
		term = normalize(term)
		if term == "" || seen[term] {
			continue
		}
		seen[term] = true
		result = append(result, term)
	}
	return result
}

func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
