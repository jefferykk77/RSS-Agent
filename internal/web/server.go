package web

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jeffery/rss-agent/internal/app"
	"github.com/jeffery/rss-agent/internal/config"
	"github.com/jeffery/rss-agent/internal/discovery"
	"github.com/jeffery/rss-agent/internal/ingest"
	"github.com/jeffery/rss-agent/internal/rss"
	"github.com/jeffery/rss-agent/internal/store"
	xsource "github.com/jeffery/rss-agent/internal/x"
)

//go:embed static/index.html
var indexHTML []byte

//go:embed static/app.js
var appJS []byte

//go:embed static/styles.css
var stylesCSS []byte

type Server struct {
	config *config.Config
	db     *store.DB

	runMu      sync.Mutex
	running    map[string]bool
	background map[string]bool
}

type feedResponse struct {
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	Tags     []string `json:"tags"`
	Disabled bool     `json:"disabled"`
}

type bootstrapResponse struct {
	Profile  string         `json:"profile"`
	Profiles []string       `json:"profiles"`
	Feeds    []feedResponse `json:"feeds"`
}

type digestResponse struct {
	Profile    string       `json:"profile"`
	Items      []digestItem `json:"items"`
	NextCursor string       `json:"next_cursor,omitempty"`
	Total      int          `json:"total"`
}

type digestItem struct {
	ID             string   `json:"id"`
	FeedName       string   `json:"feed_name"`
	FeedURL        string   `json:"feed_url"`
	Title          string   `json:"title"`
	Link           string   `json:"link"`
	Author         string   `json:"author"`
	PublishedAt    string   `json:"published_at"`
	SourceSummary  string   `json:"source_summary"`
	Content        string   `json:"content"`
	ModelLabel     string   `json:"model_label"`
	ModelName      string   `json:"model_name"`
	Score          int      `json:"score"`
	ShouldPush     bool     `json:"should_push"`
	AnalysisTitle  string   `json:"analysis_title"`
	Summary        string   `json:"summary"`
	Why            string   `json:"why"`
	KeyPoints      []string `json:"key_points"`
	Tags           []string `json:"tags"`
	AnalyzedAt     string   `json:"analyzed_at"`
	Seen           bool     `json:"seen"`
	Pushed         bool     `json:"pushed"`
	Feedback       []string `json:"feedback"`
	AnalysisStatus string   `json:"analysis_status"`
}

type feedbackRequest struct {
	Profile string `json:"profile"`
	ItemID  string `json:"item_id"`
	Action  string `json:"action"`
}

type feedbackResponse struct {
	Profile string `json:"profile"`
	ItemID  string `json:"item_id"`
	Action  string `json:"action"`
	Removed bool   `json:"removed,omitempty"`
}

type runResponse struct {
	RunID       int64    `json:"run_id"`
	Profile     string   `json:"profile"`
	Fetched     int      `json:"fetched"`
	Candidate   int      `json:"candidate"`
	Analyzed    int      `json:"analyzed"`
	Pushed      int      `json:"pushed"`
	Cached      int      `json:"cached"`
	Queued      int      `json:"queued"`
	RateLimited int      `json:"rate_limited"`
	Errors      []string `json:"errors"`
}

type analyzeResponse struct {
	Profile  string  `json:"profile"`
	ItemID   string  `json:"item_id"`
	Cached   int     `json:"cached"`
	Analyzed int     `json:"analyzed"`
	CostCNY  float64 `json:"cost_cny"`
}

type ingestRequest struct {
	Profile string   `json:"profile"`
	URL     string   `json:"url"`
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
}

type sourceHealthResponse struct {
	URL           string `json:"url"`
	Status        int    `json:"status"`
	State         string `json:"state"`
	LastError     string `json:"last_error"`
	FailCount     int    `json:"fail_count"`
	LastFetchedAt string `json:"last_fetched_at"`
	NextRetryAt   string `json:"next_retry_at"`
}

type editionResponse struct {
	ID        int64    `json:"id"`
	Slot      string   `json:"slot"`
	ItemIDs   []string `json:"item_ids"`
	Success   bool     `json:"success"`
	CreatedAt string   `json:"created_at"`
}

func New(cfg *config.Config, db *store.DB) *Server {
	server := &Server{config: cfg, db: db, running: map[string]bool{}, background: map[string]bool{}}
	for _, profileID := range cfg.ProfileNames() {
		if resolved, err := cfg.ResolveProfile(profileID); err == nil {
			_, _ = db.CleanupOldItems(context.Background(), profileID, webRetentionCutoff(time.Now(), resolved.Profile.Timezone, resolved.Settings.RetentionDays))
		}
		_, _ = db.EnsureRecoveryRun(context.Background(), profileID)
	}
	return server
}

func webRetentionCutoff(now time.Time, timezone string, days int) time.Time {
	if days <= 0 {
		days = 2
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		location = time.Local
	}
	local := now.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location).AddDate(0, 0, -(days - 1))
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/bootstrap", s.handleBootstrap)
	mux.HandleFunc("/api/digest", s.handleDigest)
	mux.HandleFunc("/api/feedback", s.handleFeedback)
	mux.HandleFunc("/api/run", s.handleRun)
	mux.HandleFunc("/api/analyze", s.handleAnalyze)
	mux.HandleFunc("/api/ingest", s.handleIngest)
	mux.HandleFunc("/api/sources/health", s.handleSourceHealth)
	mux.HandleFunc("/api/editions", s.handleEditions)
	mux.HandleFunc("/api/analysis-queue", s.handleAnalysisQueue)
	mux.HandleFunc("/api/analysis-runs/current", s.handleCurrentAnalysisRun)
	mux.HandleFunc("/api/digest/updates", s.handleDigestUpdates)
	mux.HandleFunc("/", s.handleStatic)
	return securityHeaders(mux)
}

func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var request ingestRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid ingest request: %w", err))
		return
	}
	resolved, profileID, err := s.resolveProfile(request.Profile)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	request.URL = strings.TrimSpace(request.URL)
	if request.URL == "" {
		writeError(w, http.StatusBadRequest, errors.New("url is required"))
		return
	}
	if strings.TrimSpace(request.Title) == "" || strings.TrimSpace(request.Content) == "" {
		page, fetchErr := ingest.Fetch(r.Context(), &http.Client{Timeout: resolved.HTTPTimeout()}, request.URL, 2<<20)
		if fetchErr == nil {
			if strings.TrimSpace(request.Title) == "" {
				request.Title = page.Title
			}
			if strings.TrimSpace(request.Summary) == "" {
				request.Summary = page.Description
			}
			if strings.TrimSpace(request.Content) == "" {
				request.Content = page.Content
			}
		} else if strings.TrimSpace(request.Title) == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("无法读取链接，请补充标题或正文：%w", fetchErr))
			return
		}
	}
	item := rss.Item{
		FeedName:    "投喂样本",
		FeedURL:     "manual://" + profileID,
		FeedTags:    append([]string{"manual", "interest-sample"}, request.Tags...),
		Title:       strings.TrimSpace(request.Title),
		Link:        request.URL,
		Summary:     strings.TrimSpace(request.Summary),
		Content:     strings.TrimSpace(request.Content),
		PublishedAt: time.Now(),
	}
	if err := s.db.UpsertItem(r.Context(), item); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.db.UpsertProfileItem(r.Context(), profileID, item); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"profile": profileID, "item_id": item.StableID()})
}

func (s *Server) handleSourceHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	states, err := s.db.ListSourceHealth(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	response := make([]sourceHealthResponse, 0, len(states))
	active := map[string]bool{}
	for _, profileID := range s.config.ProfileNames() {
		if resolved, resolveErr := s.config.ResolveProfile(profileID); resolveErr == nil {
			for _, feed := range resolved.EnabledFeeds() {
				active[feed.URL] = true
			}
		}
	}
	for _, state := range states {
		if !active[state.URL] {
			continue
		}
		response = append(response, sourceHealthResponse{
			URL: state.URL, Status: state.Status, LastError: state.LastError,
			State: sourceHealthState(state), FailCount: state.FailCount,
			LastFetchedAt: timeValue(state.LastFetchedAt), NextRetryAt: timeValue(state.NextRetryAt),
		})
	}
	writeJSON(w, http.StatusOK, response)
}

func sourceHealthState(state store.SourceHealth) string {
	if state.Status == http.StatusTooManyRequests && state.NextRetryAt.After(time.Now()) {
		return "rate_limited"
	}
	if state.FailCount > 0 {
		return "error"
	}
	if state.Status >= 200 && state.Status < 400 {
		return "healthy"
	}
	return "unknown"
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	resolved, profileID, err := s.resolveProfile(r.URL.Query().Get("profile"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	feeds := make([]feedResponse, 0, len(resolved.Feeds))
	for _, feed := range resolved.Feeds {
		feeds = append(feeds, feedResponse{Name: feed.Name, URL: feed.URL, Tags: feed.Tags, Disabled: feed.Disabled})
	}
	for _, source := range discovery.Sources() {
		feeds = append(feeds, feedResponse{Name: source.Name, URL: source.URL, Tags: source.Tags})
	}
	for _, search := range resolved.EnabledXSearches() {
		feeds = append(feeds, feedResponse{Name: search.Name, URL: xsource.SearchURL(search.Query), Tags: search.Tags})
	}
	writeJSON(w, http.StatusOK, bootstrapResponse{
		Profile:  profileID,
		Profiles: s.config.ProfileNames(),
		Feeds:    feeds,
	})
}

func (s *Server) handleDigest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	resolved, profileID, err := s.resolveProfile(r.URL.Query().Get("profile"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	order := strings.TrimSpace(r.URL.Query().Get("order"))
	if order == "" {
		order = "hybrid"
	}
	if order != "newest" && order != "hybrid" && order != "recommended" {
		writeError(w, http.StatusBadRequest, errors.New("order must be hybrid, newest or recommended"))
		return
	}
	page, err := s.db.DigestPageForProfile(r.Context(), profileID, resolved.ProfileHash(), r.URL.Query().Get("source"), order, limit, r.URL.Query().Get("cursor"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	items := page.Items
	response := digestResponse{Profile: profileID, Items: make([]digestItem, 0, len(items)), NextCursor: page.NextCursor, Total: page.Total}
	editionID := strings.TrimSpace(r.URL.Query().Get("edition"))
	allowed := map[string]bool{}
	if editionID != "" {
		parsedID, parseErr := strconv.ParseInt(editionID, 10, 64)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, errors.New("edition must be an integer"))
			return
		}
		editions, editionErr := s.db.DigestEditions(r.Context(), profileID, 60)
		if editionErr != nil {
			writeError(w, http.StatusInternalServerError, editionErr)
			return
		}
		for _, edition := range editions {
			if edition.ID == parsedID {
				for _, itemID := range edition.ItemIDs {
					allowed[itemID] = true
				}
			}
		}
	}
	for _, item := range items {
		if editionID != "" && !allowed[item.ID] {
			continue
		}
		if editionID == "" && !s.activeSourceURL(resolved, item.FeedURL) {
			continue
		}
		response.Items = append(response.Items, makeDigestItem(item))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) activeSourceURL(_ *config.Config, value string) bool {
	return value != "https://github.com/openai/codex/releases.atom"
}

func (s *Server) handleEditions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	_, profileID, err := s.resolveProfile(r.URL.Query().Get("profile"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	editions, err := s.db.DigestEditions(r.Context(), profileID, 30)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	response := make([]editionResponse, 0, len(editions))
	for _, edition := range editions {
		response = append(response, editionResponse{ID: edition.ID, Slot: edition.Slot, ItemIDs: nonNilStrings(edition.ItemIDs), Success: edition.Success, CreatedAt: timeValue(edition.CreatedAt)})
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleFeedback(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var request feedbackRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid feedback request: %w", err))
			return
		}
		if err := requireEOF(decoder); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		_, profileID, err := s.resolveProfile(request.Profile)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if strings.TrimSpace(request.ItemID) == "" {
			writeError(w, http.StatusBadRequest, errors.New("item_id is required"))
			return
		}
		action, err := store.ParseFeedbackAction(request.Action)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if _, err := s.db.RecordFeedbackForProfile(r.Context(), profileID, request.ItemID, action); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, feedbackResponse{Profile: profileID, ItemID: request.ItemID, Action: string(action)})
	case http.MethodDelete:
		_, profileID, err := s.resolveProfile(r.URL.Query().Get("profile"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		itemID := strings.TrimSpace(r.URL.Query().Get("item_id"))
		if itemID == "" {
			writeError(w, http.StatusBadRequest, errors.New("item_id is required"))
			return
		}
		action, err := store.ParseFeedbackAction(r.URL.Query().Get("action"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		removed, err := s.db.RemoveFeedbackForProfile(r.Context(), profileID, itemID, action)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, feedbackResponse{Profile: profileID, ItemID: itemID, Action: string(action), Removed: removed})
	default:
		methodNotAllowed(w, http.MethodPost, http.MethodDelete)
	}
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	profileName := r.URL.Query().Get("profile")
	resolved, profileID, err := s.resolveProfile(profileName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !s.startRun(profileID) {
		writeError(w, http.StatusConflict, errors.New("this profile is already running"))
		return
	}
	defer s.finishRun(profileID)

	includeSeen, err := strconv.ParseBool(firstNonEmpty(r.URL.Query().Get("include_seen"), "false"))
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("include_seen must be true or false"))
		return
	}
	summary, err := app.RunOnce(r.Context(), resolved, app.RunOptions{ProfileID: profileID, IncludeSeen: includeSeen})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, runResponse{
		RunID:       summary.RunID,
		Profile:     profileID,
		Fetched:     summary.Fetched,
		Candidate:   summary.Candidate,
		Analyzed:    summary.Analyzed,
		Pushed:      summary.Pushed,
		Cached:      summary.Cached,
		Queued:      summary.Queued,
		RateLimited: summary.RateLimited,
		Errors:      errorStrings(summary.Errors),
	})
	s.startBackground(resolved, profileID)
}

func errorStrings(values []error) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != nil {
			out = append(out, value.Error())
		}
	}
	return out
}

func (s *Server) handleAnalysisQueue(w http.ResponseWriter, r *http.Request) {
	resolved, profileID, err := s.resolveProfile(r.URL.Query().Get("profile"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		stats, err := s.db.AnalysisQueueStats(r.Context(), profileID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, stats)
	case http.MethodPost:
		var body struct {
			ItemIDs []string `json:"item_ids"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		for _, itemID := range body.ItemIDs {
			if err := s.db.PromoteAnalysis(r.Context(), profileID, resolved.ProfileHash(), itemID, 80); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		}
		s.startBackground(resolved, profileID)
		writeJSON(w, http.StatusAccepted, map[string]int{"promoted": len(body.ItemIDs)})
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleCurrentAnalysisRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	resolved, profileID, err := s.resolveProfile(r.URL.Query().Get("profile"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	run, ok, err := s.db.CurrentAnalysisRun(r.Context(), profileID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"profile": profileID, "status": "idle"})
		return
	}
	if run.Pending+run.Running+run.Retrying+run.RateLimited > 0 {
		s.startBackground(resolved, profileID)
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleDigestUpdates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	resolved, profileID, err := s.resolveProfile(r.URL.Query().Get("profile"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ids := r.URL.Query()["item_id"]
	if len(ids) > 100 {
		writeError(w, http.StatusBadRequest, errors.New("at most 100 item_id values are allowed"))
		return
	}
	items, err := s.db.DigestItemsByIDsForProfile(r.Context(), profileID, resolved.ProfileHash(), ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	response := digestResponse{Profile: profileID, Items: make([]digestItem, 0, len(items))}
	for _, item := range items {
		response.Items = append(response.Items, makeDigestItem(item))
	}
	writeJSON(w, http.StatusOK, response)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (s *Server) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	resolved, profileID, err := s.resolveProfile(r.URL.Query().Get("profile"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	itemID := strings.TrimSpace(r.URL.Query().Get("item_id"))
	if itemID == "" {
		writeError(w, http.StatusBadRequest, errors.New("item_id is required"))
		return
	}
	if !s.startRun(profileID) {
		writeError(w, http.StatusConflict, errors.New("this profile is already running"))
		return
	}
	defer s.finishRun(profileID)

	summary, err := app.AnalyzeSavedItem(r.Context(), resolved, profileID, itemID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, analyzeResponse{
		Profile:  profileID,
		ItemID:   itemID,
		Cached:   summary.Cached,
		Analyzed: summary.Analyzed,
		CostCNY:  summary.LLMCostCNY,
	})
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	switch r.URL.Path {
	case "/", "/index.html":
		serveAsset(w, r, "text/html; charset=utf-8", indexHTML)
	case "/app.js":
		serveAsset(w, r, "text/javascript; charset=utf-8", appJS)
	case "/styles.css":
		serveAsset(w, r, "text/css; charset=utf-8", stylesCSS)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) resolveProfile(name string) (*config.Config, string, error) {
	if s.config == nil {
		return nil, "", errors.New("web server configuration is unavailable")
	}
	profileID := strings.TrimSpace(name)
	if profileID == "" {
		profileID = config.DefaultProfileName
	}
	resolved, err := s.config.ResolveProfile(profileID)
	if err != nil {
		return nil, "", err
	}
	return resolved, profileID, nil
}

func (s *Server) startRun(profileID string) bool {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.running[profileID] {
		return false
	}
	s.running[profileID] = true
	return true
}

func (s *Server) finishRun(profileID string) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	delete(s.running, profileID)
}

func (s *Server) startBackground(cfg *config.Config, profileID string) {
	s.runMu.Lock()
	if s.background[profileID] {
		s.runMu.Unlock()
		return
	}
	s.background[profileID] = true
	s.runMu.Unlock()
	go func() {
		defer func() { s.runMu.Lock(); delete(s.background, profileID); s.runMu.Unlock() }()
		_ = app.DrainAnalysisQueue(context.Background(), cfg, profileID)
	}()
}

func makeDigestItem(item store.DigestItem) digestItem {
	feedback := make([]string, len(item.Feedback))
	for i, action := range item.Feedback {
		feedback[i] = string(action)
	}
	return digestItem{
		ID:            item.ID,
		FeedName:      item.FeedName,
		FeedURL:       item.FeedURL,
		Title:         item.Title,
		Link:          item.Link,
		Author:        item.Author,
		PublishedAt:   timeValue(item.PublishedAt),
		SourceSummary: item.SourceSummary,
		Content:       item.Content,
		ModelLabel:    item.ModelLabel,
		ModelName:     item.ModelName,
		Score:         item.Score,
		ShouldPush:    item.ShouldPush,
		AnalysisTitle: item.AnalysisTitle,
		Summary:       item.Summary,
		Why:           item.Why,
		KeyPoints:     nonNilStrings(item.KeyPoints),
		Tags:          nonNilStrings(item.Tags),
		AnalyzedAt:    timeValue(item.AnalyzedAt),
		Seen:          item.Seen,
		Pushed:        item.Pushed,
		Feedback:      feedback,
		AnalysisStatus: func() string {
			if item.AnalyzedAt.IsZero() {
				return "pending"
			}
			return "completed"
		}(),
	}
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func parseLimit(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 30, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 200 {
		return 0, errors.New("limit must be between 1 and 200")
	}
	return limit, nil
}

func timeValue(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func requireEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("feedback request must contain one JSON object")
	}
	return nil
}

func serveAsset(w http.ResponseWriter, r *http.Request, contentType string, content []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(content)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func methodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}
