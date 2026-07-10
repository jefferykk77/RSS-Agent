package web

import (
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
	"github.com/jeffery/rss-agent/internal/store"
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

	runMu   sync.Mutex
	running map[string]bool
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
	Profile string       `json:"profile"`
	Items   []digestItem `json:"items"`
}

type digestItem struct {
	ID            string   `json:"id"`
	FeedName      string   `json:"feed_name"`
	FeedURL       string   `json:"feed_url"`
	Title         string   `json:"title"`
	Link          string   `json:"link"`
	Author        string   `json:"author"`
	PublishedAt   string   `json:"published_at"`
	SourceSummary string   `json:"source_summary"`
	Content       string   `json:"content"`
	ModelLabel    string   `json:"model_label"`
	ModelName     string   `json:"model_name"`
	Score         int      `json:"score"`
	ShouldPush    bool     `json:"should_push"`
	AnalysisTitle string   `json:"analysis_title"`
	Summary       string   `json:"summary"`
	Why           string   `json:"why"`
	KeyPoints     []string `json:"key_points"`
	Tags          []string `json:"tags"`
	AnalyzedAt    string   `json:"analyzed_at"`
	Seen          bool     `json:"seen"`
	Pushed        bool     `json:"pushed"`
	Feedback      []string `json:"feedback"`
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
	Profile   string `json:"profile"`
	Fetched   int    `json:"fetched"`
	Candidate int    `json:"candidate"`
	Analyzed  int    `json:"analyzed"`
	Pushed    int    `json:"pushed"`
}

func New(cfg *config.Config, db *store.DB) *Server {
	return &Server{config: cfg, db: db, running: map[string]bool{}}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/bootstrap", s.handleBootstrap)
	mux.HandleFunc("/api/digest", s.handleDigest)
	mux.HandleFunc("/api/feedback", s.handleFeedback)
	mux.HandleFunc("/api/run", s.handleRun)
	mux.HandleFunc("/", s.handleStatic)
	return securityHeaders(mux)
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
	items, err := s.db.DigestItemsForProfile(r.Context(), profileID, resolved.ProfileHash(), limit)
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

	summary, err := app.RunOnce(r.Context(), resolved, app.RunOptions{ProfileID: profileID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, runResponse{
		Profile:   profileID,
		Fetched:   summary.Fetched,
		Candidate: summary.Candidate,
		Analyzed:  summary.Analyzed,
		Pushed:    summary.Pushed,
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
		KeyPoints:     item.KeyPoints,
		Tags:          item.Tags,
		AnalyzedAt:    timeValue(item.AnalyzedAt),
		Seen:          item.Seen,
		Pushed:        item.Pushed,
		Feedback:      feedback,
	}
}

func parseLimit(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 100, nil
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
