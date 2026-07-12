package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jeffery/rss-agent/internal/agent"
	"github.com/jeffery/rss-agent/internal/rss"
	_ "modernc.org/sqlite"
)

type DB struct {
	sql *sql.DB
}

const defaultProfileID = "default"

type CostEvent struct {
	Scope        string
	Provider     string
	Model        string
	ModelLabel   string
	Kind         string
	InputTokens  int
	OutputTokens int
	CostCNY      float64
	CreatedAt    time.Time
}

type FeedbackAction string

const (
	FeedbackLike           FeedbackAction = "like"
	FeedbackDislike        FeedbackAction = "dislike"
	FeedbackBlock          FeedbackAction = "block"
	FeedbackSave           FeedbackAction = "save"
	FeedbackLater          FeedbackAction = "later"
	FeedbackBlockFeed      FeedbackAction = "block-feed"
	FeedbackTooShallow     FeedbackAction = "too-shallow"
	FeedbackTooTheoretical FeedbackAction = "too-theoretical"
	FeedbackTooMarketing   FeedbackAction = "too-marketing"
	FeedbackUnusable       FeedbackAction = "unusable"
	FeedbackMoreLikeThis   FeedbackAction = "more-like-this"
)

type Feedback struct {
	ItemID      string
	Action      FeedbackAction
	TargetValue string
	Title       string
	Link        string
	FeedName    string
	CreatedAt   time.Time
}

type FeedbackFilters struct {
	BlockedItemIDs  map[string]bool
	BlockedFeedURLs map[string]bool
}

type RecentItem struct {
	ID          string
	Title       string
	Link        string
	FeedName    string
	PublishedAt time.Time
}

type DigestItem struct {
	ID            string
	FeedName      string
	FeedURL       string
	Title         string
	Link          string
	Author        string
	PublishedAt   time.Time
	SourceSummary string
	Content       string
	ModelLabel    string
	ModelName     string
	Score         int
	ShouldPush    bool
	AnalysisTitle string
	Summary       string
	Why           string
	KeyPoints     []string
	Tags          []string
	AnalyzedAt    time.Time
	Seen          bool
	Pushed        bool
	Feedback      []FeedbackAction
}

type SourceHealth struct {
	URL           string
	Status        int
	LastError     string
	FailCount     int
	LastFetchedAt time.Time
	NextRetryAt   time.Time
}

type DigestEdition struct {
	ID        int64
	ProfileID string
	Slot      string
	ItemIDs   []string
	Success   bool
	CreatedAt time.Time
}

type AnalysisTask struct {
	ID          int64
	RunID       int64
	ProfileID   string
	ProfileHash string
	ItemID      string
	ContentHash string
	Priority    int
	Status      string
	Attempts    int
	NextRetryAt time.Time
	LastError   string
}

type AnalysisQueueStats struct {
	Completed   int `json:"completed"`
	Pending     int `json:"pending"`
	Running     int `json:"running"`
	Retrying    int `json:"retrying"`
	RateLimited int `json:"rate_limited"`
	Failed      int `json:"failed"`
}

type AnalysisRun struct {
	ID          int64     `json:"run_id"`
	ProfileID   string    `json:"profile"`
	Status      string    `json:"status"`
	Total       int       `json:"total"`
	Cached      int       `json:"cached"`
	Analyzed    int       `json:"analyzed"`
	Pending     int       `json:"pending"`
	Running     int       `json:"running"`
	Retrying    int       `json:"retrying"`
	RateLimited int       `json:"rate_limited"`
	Failed      int       `json:"failed"`
	CreatedAt   time.Time `json:"created_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
}

type DigestPage struct {
	Items      []DigestItem
	NextCursor string
	Total      int
}

func (db *DB) DigestPageForProfile(ctx context.Context, profileID, profileHash, source, order string, limit int, cursor string) (DigestPage, error) {
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}
	all, err := db.DigestItemsForProfile(ctx, profileID, profileHash, 5000)
	if err != nil {
		return DigestPage{}, err
	}
	visible := all[:0]
	for _, item := range all {
		if item.FeedURL != "https://github.com/openai/codex/releases.atom" {
			visible = append(visible, item)
		}
	}
	all = visible
	if source != "" {
		filtered := all[:0]
		for _, item := range all {
			if item.FeedURL == source {
				filtered = append(filtered, item)
			}
		}
		all = filtered
	}
	if order == "recommended" {
		filtered := all[:0]
		for _, item := range all {
			if !item.AnalyzedAt.IsZero() && item.ShouldPush {
				filtered = append(filtered, item)
			}
		}
		all = filtered
		sort.SliceStable(all, func(i, j int) bool {
			if all[i].Score == all[j].Score {
				return all[i].PublishedAt.After(all[j].PublishedAt)
			}
			return all[i].Score > all[j].Score
		})
	} else if order == "newest" {
		sort.SliceStable(all, func(i, j int) bool { return all[i].PublishedAt.After(all[j].PublishedAt) })
	} else {
		sort.SliceStable(all, func(i, j int) bool {
			ai, aj := !all[i].AnalyzedAt.IsZero(), !all[j].AnalyzedAt.IsZero()
			if ai != aj {
				return ai
			}
			if ai && all[i].Score != all[j].Score {
				return all[i].Score > all[j].Score
			}
			return all[i].PublishedAt.After(all[j].PublishedAt)
		})
	}
	offset := 0
	if cursor != "" {
		_, _ = fmt.Sscanf(cursor, "%d", &offset)
	}
	if offset < 0 || offset > len(all) {
		offset = 0
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	page := DigestPage{Items: append([]DigestItem(nil), all[offset:end]...), Total: len(all)}
	if end < len(all) {
		page.NextCursor = fmt.Sprintf("%d", end)
	}
	return page, nil
}

func ParseFeedbackAction(raw string) (FeedbackAction, error) {
	action := FeedbackAction(strings.ToLower(strings.TrimSpace(raw)))
	if !action.Valid() {
		return "", fmt.Errorf("不支持的反馈操作 %q", raw)
	}
	return action, nil
}

func (a FeedbackAction) Valid() bool {
	switch a {
	case FeedbackLike, FeedbackDislike, FeedbackBlock, FeedbackSave, FeedbackLater, FeedbackBlockFeed,
		FeedbackTooShallow, FeedbackTooTheoretical, FeedbackTooMarketing, FeedbackUnusable, FeedbackMoreLikeThis:
		return true
	default:
		return false
	}
}

func Open(path string) (*DB, error) {
	if path == "" {
		path = ".rss-agent/rss-agent.db"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db := &DB{sql: conn}
	if err := db.configure(); err != nil {
		conn.Close()
		return nil, err
	}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, err
	}
	return db, nil
}

func (db *DB) Close() error {
	if db == nil || db.sql == nil {
		return nil
	}
	return db.sql.Close()
}

func (db *DB) configure() error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	}
	for _, stmt := range pragmas {
		if _, err := db.sql.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS feed_fetch_states (
			feed_url TEXT PRIMARY KEY,
			etag TEXT,
			last_modified TEXT,
			last_status INTEGER NOT NULL DEFAULT 0,
			last_error TEXT,
			fail_count INTEGER NOT NULL DEFAULT 0,
			last_fetched_at TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS items (
			id TEXT PRIMARY KEY,
			feed_name TEXT NOT NULL,
			feed_url TEXT NOT NULL,
			feed_tags_json TEXT,
			title TEXT,
			link TEXT,
			guid TEXT,
			author TEXT,
			categories_json TEXT,
			published_at TEXT,
			updated_at TEXT,
			summary TEXT,
			content TEXT,
			content_hash TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_db_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_items_feed_url ON items(feed_url)`,
		`CREATE INDEX IF NOT EXISTS idx_items_content_hash ON items(content_hash)`,
		`CREATE TABLE IF NOT EXISTS profile_items (
			profile_id TEXT NOT NULL,
			item_id TEXT NOT NULL,
			added_at TEXT NOT NULL,
			PRIMARY KEY (profile_id, item_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_profile_items_recent ON profile_items(profile_id, added_at DESC)`,
		`CREATE TABLE IF NOT EXISTS seen_items (
			item_id TEXT PRIMARY KEY,
			title TEXT,
			link TEXT,
			feed_name TEXT,
			seen_at TEXT NOT NULL,
			pushed INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS profile_seen_items (
			profile_id TEXT NOT NULL,
			item_id TEXT NOT NULL,
			seen_at TEXT NOT NULL,
			pushed INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (profile_id, item_id)
		)`,
		`CREATE TABLE IF NOT EXISTS item_analyses (
			item_id TEXT NOT NULL,
			profile_hash TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			model_label TEXT,
			model_name TEXT,
			score INTEGER NOT NULL,
			should_push INTEGER NOT NULL,
			title TEXT,
			summary TEXT,
			why TEXT,
			key_points_json TEXT,
			tags_json TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (item_id, profile_hash, content_hash)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_item_analyses_profile ON item_analyses(profile_hash, created_at)`,
		`CREATE TABLE IF NOT EXISTS push_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			item_id TEXT NOT NULL,
			channel TEXT NOT NULL,
			success INTEGER NOT NULL,
			error TEXT,
			pushed_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_push_records_item ON push_records(item_id)`,
		`CREATE TABLE IF NOT EXISTS item_feedback (
			item_id TEXT NOT NULL,
			action TEXT NOT NULL CHECK (action IN ('like', 'dislike', 'block', 'save', 'later', 'block-feed')),
			target_value TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY (item_id, action)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_item_feedback_action_created ON item_feedback(action, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS profile_feedback (
			profile_id TEXT NOT NULL,
			item_id TEXT NOT NULL,
			action TEXT NOT NULL CHECK (action IN ('like', 'dislike', 'block', 'save', 'later', 'block-feed')),
			target_value TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY (profile_id, item_id, action)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_profile_feedback_action_created ON profile_feedback(profile_id, action, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS profile_feedback_signals (
			profile_id TEXT NOT NULL,
			item_id TEXT NOT NULL,
			action TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY (profile_id, item_id, action)
		)`,
		`CREATE TABLE IF NOT EXISTS cost_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			scope TEXT NOT NULL,
			provider TEXT,
			model TEXT,
			model_label TEXT,
			kind TEXT,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			cost_cny REAL NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cost_events_scope_created ON cost_events(scope, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_cost_events_model_created ON cost_events(provider, model, created_at)`,
		`CREATE TABLE IF NOT EXISTS digest_editions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			profile_id TEXT NOT NULL,
			slot TEXT NOT NULL,
			item_ids_json TEXT NOT NULL,
			success INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_digest_editions_profile_created ON digest_editions(profile_id, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS analysis_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			profile_id TEXT NOT NULL,
			profile_hash TEXT NOT NULL,
			item_id TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			prompt_version TEXT NOT NULL,
			priority INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'pending',
			attempts INTEGER NOT NULL DEFAULT 0,
			next_retry_at TEXT,
			last_error TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(item_id, profile_hash, content_hash, prompt_version)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_analysis_tasks_ready ON analysis_tasks(profile_id, status, priority DESC, created_at)`,
		`CREATE TABLE IF NOT EXISTS analysis_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			profile_id TEXT NOT NULL,
			status TEXT NOT NULL,
			total INTEGER NOT NULL DEFAULT 0,
			cached INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			completed_at TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_analysis_runs_profile_created ON analysis_runs(profile_id, created_at DESC)`,
	}
	for _, stmt := range stmts {
		if _, err := db.sql.Exec(stmt); err != nil {
			return err
		}
	}
	if err := db.ensureColumn("analysis_tasks", "run_id", "INTEGER"); err != nil {
		return err
	}
	if err := db.ensureColumn("feed_fetch_states", "next_retry_at", "TEXT"); err != nil {
		return err
	}
	legacyMigrations := []string{
		`INSERT OR IGNORE INTO profile_items (profile_id, item_id, added_at)
			SELECT 'default', id, updated_db_at FROM items`,
		`INSERT OR IGNORE INTO profile_seen_items (profile_id, item_id, seen_at, pushed)
			SELECT 'default', item_id, seen_at, pushed FROM seen_items`,
		`INSERT OR IGNORE INTO profile_feedback (profile_id, item_id, action, target_value, created_at)
			SELECT 'default', item_id, action, target_value, created_at FROM item_feedback`,
	}
	for _, stmt := range legacyMigrations {
		if _, err := db.sql.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) ensureColumn(table, column, definition string) error {
	rows, err := db.sql.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	_, err = db.sql.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + definition)
	return err
}

func (db *DB) GetFeedState(ctx context.Context, feedURL string) (rss.FeedFetchState, bool, error) {
	row := db.sql.QueryRowContext(ctx, `SELECT feed_url, etag, last_modified, last_status, last_error, fail_count, last_fetched_at, COALESCE(next_retry_at, '')
		FROM feed_fetch_states WHERE feed_url = ?`, feedURL)
	var state rss.FeedFetchState
	var fetchedAt, retryAt string
	if err := row.Scan(&state.FeedURL, &state.ETag, &state.LastModified, &state.LastStatus, &state.LastError, &state.FailCount, &fetchedAt, &retryAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return rss.FeedFetchState{}, false, nil
		}
		return rss.FeedFetchState{}, false, err
	}
	state.LastFetchedAt = parseTime(fetchedAt)
	state.NextRetryAt = parseTime(retryAt)
	return state, true, nil
}

func (db *DB) SaveFeedState(ctx context.Context, state rss.FeedFetchState) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.sql.ExecContext(ctx, `INSERT INTO feed_fetch_states
		(feed_url, etag, last_modified, last_status, last_error, fail_count, last_fetched_at, next_retry_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(feed_url) DO UPDATE SET
			etag = excluded.etag,
			last_modified = excluded.last_modified,
			last_status = excluded.last_status,
			last_error = excluded.last_error,
			fail_count = excluded.fail_count,
			last_fetched_at = excluded.last_fetched_at,
			next_retry_at = excluded.next_retry_at,
			updated_at = excluded.updated_at`,
		state.FeedURL,
		state.ETag,
		state.LastModified,
		state.LastStatus,
		state.LastError,
		state.FailCount,
		formatTime(state.LastFetchedAt),
		formatTime(state.NextRetryAt),
		now,
	)
	return err
}

func (db *DB) ListSourceHealth(ctx context.Context) ([]SourceHealth, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT feed_url, last_status, COALESCE(last_error, ''), fail_count, COALESCE(last_fetched_at, ''), COALESCE(next_retry_at, '')
		FROM feed_fetch_states ORDER BY last_fetched_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []SourceHealth
	for rows.Next() {
		var item SourceHealth
		var fetched, retryAt string
		if err := rows.Scan(&item.URL, &item.Status, &item.LastError, &item.FailCount, &fetched, &retryAt); err != nil {
			return nil, err
		}
		item.LastFetchedAt = parseTime(fetched)
		item.NextRetryAt = parseTime(retryAt)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (db *DB) RecordDigestEdition(ctx context.Context, profileID string, slot string, itemIDs []string, success bool) error {
	encoded, err := json.Marshal(itemIDs)
	if err != nil {
		return err
	}
	_, err = db.sql.ExecContext(ctx, `INSERT INTO digest_editions (profile_id, slot, item_ids_json, success, created_at)
		VALUES (?, ?, ?, ?, ?)`, normalizeProfileID(profileID), slot, string(encoded), boolInt(success), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (db *DB) DigestEditions(ctx context.Context, profileID string, limit int) ([]DigestEdition, error) {
	if limit <= 0 || limit > 60 {
		limit = 30
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT id, profile_id, slot, item_ids_json, success, created_at
		FROM digest_editions WHERE profile_id = ? ORDER BY created_at DESC LIMIT ?`, normalizeProfileID(profileID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var editions []DigestEdition
	for rows.Next() {
		var edition DigestEdition
		var encoded, created string
		var success int
		if err := rows.Scan(&edition.ID, &edition.ProfileID, &edition.Slot, &encoded, &success, &created); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(encoded), &edition.ItemIDs); err != nil {
			return nil, err
		}
		edition.Success = success != 0
		edition.CreatedAt = parseTime(created)
		editions = append(editions, edition)
	}
	return editions, rows.Err()
}

func (db *DB) DigestItemCountSince(ctx context.Context, profileID string, since time.Time) (int, error) {
	editions, err := db.DigestEditions(ctx, profileID, 60)
	if err != nil {
		return 0, err
	}
	seen := map[string]bool{}
	for _, edition := range editions {
		if !edition.Success || edition.CreatedAt.Before(since) {
			continue
		}
		for _, id := range edition.ItemIDs {
			seen[id] = true
		}
	}
	return len(seen), nil
}

func (db *DB) UpsertItem(ctx context.Context, item rss.Item) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	feedTags, err := json.Marshal(item.FeedTags)
	if err != nil {
		return err
	}
	categories, err := json.Marshal(item.Categories)
	if err != nil {
		return err
	}
	_, err = db.sql.ExecContext(ctx, `INSERT INTO items
		(id, feed_name, feed_url, feed_tags_json, title, link, guid, author, categories_json, published_at, updated_at, summary, content, content_hash, created_at, updated_db_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			feed_name = excluded.feed_name,
			feed_url = excluded.feed_url,
			feed_tags_json = excluded.feed_tags_json,
			title = excluded.title,
			link = excluded.link,
			guid = excluded.guid,
			author = excluded.author,
			categories_json = excluded.categories_json,
			published_at = excluded.published_at,
			updated_at = excluded.updated_at,
			summary = excluded.summary,
			content = excluded.content,
			content_hash = excluded.content_hash,
			updated_db_at = excluded.updated_db_at`,
		item.StableID(),
		item.FeedName,
		item.FeedURL,
		string(feedTags),
		item.Title,
		item.Link,
		item.GUID,
		item.Author,
		string(categories),
		formatTime(item.PublishedAt),
		formatTime(item.UpdatedAt),
		item.Summary,
		item.Content,
		item.ContentHash(),
		now,
		now,
	)
	return err
}

func (db *DB) UpsertProfileItem(ctx context.Context, profileID string, item rss.Item) error {
	profileID = normalizeProfileID(profileID)
	_, err := db.sql.ExecContext(ctx, `INSERT INTO profile_items (profile_id, item_id, added_at)
		VALUES (?, ?, ?)
		ON CONFLICT(profile_id, item_id) DO UPDATE SET added_at = excluded.added_at`,
		profileID,
		item.StableID(),
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (db *DB) UpsertItemsForProfile(ctx context.Context, profileID string, items []rss.Item) error {
	if len(items) == 0 {
		return nil
	}
	profileID = normalizeProfileID(profileID)
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, item := range items {
		feedTags, err := json.Marshal(item.FeedTags)
		if err != nil {
			return err
		}
		categories, err := json.Marshal(item.Categories)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO items(id,feed_name,feed_url,feed_tags_json,title,link,guid,author,categories_json,published_at,updated_at,summary,content,content_hash,created_at,updated_db_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET feed_name=excluded.feed_name,feed_url=excluded.feed_url,feed_tags_json=excluded.feed_tags_json,title=excluded.title,link=excluded.link,guid=excluded.guid,author=excluded.author,categories_json=excluded.categories_json,published_at=excluded.published_at,updated_at=excluded.updated_at,summary=excluded.summary,content=excluded.content,content_hash=excluded.content_hash,updated_db_at=excluded.updated_db_at`, item.StableID(), item.FeedName, item.FeedURL, string(feedTags), item.Title, item.Link, item.GUID, item.Author, string(categories), formatTime(item.PublishedAt), formatTime(item.UpdatedAt), item.Summary, item.Content, item.ContentHash(), now, now); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO profile_items(profile_id,item_id,added_at) VALUES(?,?,?) ON CONFLICT(profile_id,item_id) DO UPDATE SET added_at=excluded.added_at`, profileID, item.StableID(), now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (db *DB) CleanupOldItems(ctx context.Context, profileID string, cutoff time.Time) (int, error) {
	profileID = normalizeProfileID(profileID)
	protected := map[string]bool{}
	rows, err := db.sql.QueryContext(ctx, `SELECT item_id FROM profile_feedback WHERE profile_id=? AND action IN ('save','later')`, profileID)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		protected[id] = true
	}
	rows.Close()
	editions, err := db.DigestEditions(ctx, profileID, 10000)
	if err != nil {
		return 0, err
	}
	for _, edition := range editions {
		for _, id := range edition.ItemIDs {
			protected[id] = true
		}
	}
	rows, err = db.sql.QueryContext(ctx, `SELECT i.id,i.feed_url FROM profile_items p JOIN items i ON i.id=p.item_id WHERE p.profile_id=? AND COALESCE(NULLIF(i.published_at,''),NULLIF(i.updated_at,''),i.created_at) < ?`, profileID, cutoff.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id, feedURL string
		if err := rows.Scan(&id, &feedURL); err != nil {
			rows.Close()
			return 0, err
		}
		if !protected[id] && !strings.HasPrefix(feedURL, "manual://") {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if len(ids) == 0 {
		return 0, nil
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err = tx.ExecContext(ctx, `DELETE FROM profile_items WHERE profile_id=? AND item_id=?`, profileID, id); err != nil {
			return 0, err
		}
		var refs int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM profile_items WHERE item_id=?`, id).Scan(&refs); err != nil {
			return 0, err
		}
		if refs == 0 {
			for _, query := range []string{`DELETE FROM analysis_tasks WHERE item_id=?`, `DELETE FROM item_analyses WHERE item_id=?`, `DELETE FROM profile_seen_items WHERE item_id=?`, `DELETE FROM profile_feedback WHERE item_id=?`, `DELETE FROM item_feedback WHERE item_id=?`, `DELETE FROM items WHERE id=?`} {
				if _, err = tx.ExecContext(ctx, query, id); err != nil {
					return 0, err
				}
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func (db *DB) ItemForProfile(ctx context.Context, profileID string, itemID string) (rss.Item, error) {
	profileID = normalizeProfileID(profileID)
	row := db.sql.QueryRowContext(ctx, `SELECT
		i.id, i.feed_name, i.feed_url, COALESCE(i.feed_tags_json, '[]'),
		COALESCE(i.title, ''), COALESCE(i.link, ''), COALESCE(i.guid, ''), COALESCE(i.author, ''),
		COALESCE(i.categories_json, '[]'), COALESCE(i.published_at, ''), COALESCE(i.updated_at, ''),
		COALESCE(i.summary, ''), COALESCE(i.content, '')
		FROM profile_items p
		JOIN items i ON i.id = p.item_id
		WHERE p.profile_id = ? AND p.item_id = ?`, profileID, itemID)
	var (
		item           rss.Item
		feedTagsJSON   string
		categoriesJSON string
		publishedRaw   string
		updatedRaw     string
	)
	if err := row.Scan(
		&item.ID,
		&item.FeedName,
		&item.FeedURL,
		&feedTagsJSON,
		&item.Title,
		&item.Link,
		&item.GUID,
		&item.Author,
		&categoriesJSON,
		&publishedRaw,
		&updatedRaw,
		&item.Summary,
		&item.Content,
	); err != nil {
		return rss.Item{}, err
	}
	if err := json.Unmarshal([]byte(feedTagsJSON), &item.FeedTags); err != nil {
		return rss.Item{}, fmt.Errorf("decode feed tags: %w", err)
	}
	if err := json.Unmarshal([]byte(categoriesJSON), &item.Categories); err != nil {
		return rss.Item{}, fmt.Errorf("decode categories: %w", err)
	}
	item.PublishedAt = parseTime(publishedRaw)
	item.UpdatedAt = parseTime(updatedRaw)
	return item, nil
}

func (db *DB) SeenIDs(ctx context.Context) (map[string]bool, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT item_id FROM seen_items`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}

func (db *DB) SeenIDsForProfile(ctx context.Context, profileID string) (map[string]bool, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT item_id FROM profile_seen_items WHERE profile_id = ?`, normalizeProfileID(profileID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}

func (db *DB) IsSeen(ctx context.Context, id string) (bool, error) {
	var exists int
	err := db.sql.QueryRowContext(ctx, `SELECT 1 FROM seen_items WHERE item_id = ?`, id).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (db *DB) MarkSeen(ctx context.Context, item rss.Item, pushed bool) error {
	_, err := db.sql.ExecContext(ctx, `INSERT INTO seen_items
		(item_id, title, link, feed_name, seen_at, pushed)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(item_id) DO UPDATE SET
			title = excluded.title,
			link = excluded.link,
			feed_name = excluded.feed_name,
			seen_at = excluded.seen_at,
			pushed = excluded.pushed`,
		item.StableID(),
		item.Title,
		item.Link,
		item.FeedName,
		time.Now().UTC().Format(time.RFC3339Nano),
		boolInt(pushed),
	)
	return err
}

func (db *DB) MarkSeenForProfile(ctx context.Context, profileID string, item rss.Item, pushed bool) error {
	_, err := db.sql.ExecContext(ctx, `INSERT INTO profile_seen_items
		(profile_id, item_id, seen_at, pushed)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(profile_id, item_id) DO UPDATE SET
			seen_at = excluded.seen_at,
			pushed = excluded.pushed`,
		normalizeProfileID(profileID),
		item.StableID(),
		time.Now().UTC().Format(time.RFC3339Nano),
		boolInt(pushed),
	)
	return err
}

func (db *DB) CachedAnalysis(ctx context.Context, item rss.Item, profileHash string, ttl time.Duration) (agent.Result, bool, error) {
	row := db.sql.QueryRowContext(ctx, `SELECT model_label, model_name, score, should_push, title, summary, why, key_points_json, tags_json, created_at
		FROM item_analyses WHERE item_id = ? AND profile_hash = ? AND content_hash = ?`,
		item.StableID(), profileHash, item.ContentHash())
	var (
		modelLabel    string
		modelName     string
		decision      agent.Decision
		shouldPush    int
		keyPointsJSON string
		tagsJSON      string
		createdAtRaw  string
	)
	if err := row.Scan(&modelLabel, &modelName, &decision.Score, &shouldPush, &decision.Title, &decision.Summary, &decision.Why, &keyPointsJSON, &tagsJSON, &createdAtRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return agent.Result{}, false, nil
		}
		return agent.Result{}, false, err
	}
	createdAt := parseTime(createdAtRaw)
	if ttl > 0 && !createdAt.IsZero() && createdAt.Before(time.Now().Add(-ttl)) {
		return agent.Result{}, false, nil
	}
	decision.ItemID = item.StableID()
	decision.ShouldPush = shouldPush == 1
	if err := json.Unmarshal([]byte(keyPointsJSON), &decision.KeyPoints); err != nil {
		return agent.Result{}, false, err
	}
	if err := json.Unmarshal([]byte(tagsJSON), &decision.Tags); err != nil {
		return agent.Result{}, false, err
	}
	return agent.Result{Item: item, Decision: decision, ModelLabel: modelLabel, ModelName: modelName, Cached: true}, true, nil
}

func (db *DB) SaveAnalysis(ctx context.Context, item rss.Item, profileHash string, modelLabel string, modelName string, decision agent.Decision) error {
	keyPoints, err := json.Marshal(decision.KeyPoints)
	if err != nil {
		return err
	}
	tags, err := json.Marshal(decision.Tags)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.sql.ExecContext(ctx, `INSERT INTO item_analyses
		(item_id, profile_hash, content_hash, model_label, model_name, score, should_push, title, summary, why, key_points_json, tags_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(item_id, profile_hash, content_hash) DO UPDATE SET
			model_label = excluded.model_label,
			model_name = excluded.model_name,
			score = excluded.score,
			should_push = excluded.should_push,
			title = excluded.title,
			summary = excluded.summary,
			why = excluded.why,
			key_points_json = excluded.key_points_json,
			tags_json = excluded.tags_json,
			updated_at = excluded.updated_at`,
		item.StableID(),
		profileHash,
		item.ContentHash(),
		modelLabel,
		modelName,
		decision.Score,
		boolInt(decision.ShouldPush),
		decision.Title,
		decision.Summary,
		decision.Why,
		string(keyPoints),
		string(tags),
		now,
		now,
	)
	return err
}

func (db *DB) RecordPush(ctx context.Context, itemID string, channel string, success bool, errText string) error {
	_, err := db.sql.ExecContext(ctx, `INSERT INTO push_records (item_id, channel, success, error, pushed_at)
		VALUES (?, ?, ?, ?, ?)`,
		itemID, channel, boolInt(success), errText, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (db *DB) RecordFeedback(ctx context.Context, itemID string, action FeedbackAction) (Feedback, error) {
	if !action.Valid() {
		return Feedback{}, fmt.Errorf("不支持的反馈操作 %q", action)
	}

	feedback := Feedback{ItemID: itemID, Action: action}
	var feedURL string
	err := db.sql.QueryRowContext(ctx, `SELECT title, link, feed_name, feed_url FROM items WHERE id = ?`, itemID).
		Scan(&feedback.Title, &feedback.Link, &feedback.FeedName, &feedURL)
	if errors.Is(err, sql.ErrNoRows) {
		return Feedback{}, fmt.Errorf("未找到条目 %q；请先用常规 once 运行保存条目", itemID)
	}
	if err != nil {
		return Feedback{}, err
	}

	feedback.TargetValue = itemID
	if action == FeedbackBlockFeed {
		if strings.TrimSpace(feedURL) == "" {
			return Feedback{}, fmt.Errorf("条目 %q 没有可屏蔽的订阅源", itemID)
		}
		feedback.TargetValue = feedURL
	}
	if action == FeedbackLike {
		if _, err := db.sql.ExecContext(ctx, `DELETE FROM item_feedback WHERE item_id = ? AND action = ?`, itemID, FeedbackDislike); err != nil {
			return Feedback{}, err
		}
	}
	if action == FeedbackDislike {
		if _, err := db.sql.ExecContext(ctx, `DELETE FROM item_feedback WHERE item_id = ? AND action = ?`, itemID, FeedbackLike); err != nil {
			return Feedback{}, err
		}
	}

	feedback.CreatedAt = time.Now().UTC()
	_, err = db.sql.ExecContext(ctx, `INSERT INTO item_feedback (item_id, action, target_value, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(item_id, action) DO UPDATE SET
			target_value = excluded.target_value,
			created_at = excluded.created_at`,
		feedback.ItemID,
		feedback.Action,
		feedback.TargetValue,
		feedback.CreatedAt.Format(time.RFC3339Nano),
	)
	return feedback, err
}

func (db *DB) RecordFeedbackForProfile(ctx context.Context, profileID string, itemID string, action FeedbackAction) (Feedback, error) {
	if !action.Valid() {
		return Feedback{}, fmt.Errorf("不支持的反馈操作 %q", action)
	}
	profileID = normalizeProfileID(profileID)

	feedback := Feedback{ItemID: itemID, Action: action}
	var feedURL string
	err := db.sql.QueryRowContext(ctx, `SELECT i.title, i.link, i.feed_name, i.feed_url
		FROM profile_items p JOIN items i ON i.id = p.item_id
		WHERE p.profile_id = ? AND p.item_id = ?`, profileID, itemID).
		Scan(&feedback.Title, &feedback.Link, &feedback.FeedName, &feedURL)
	if errors.Is(err, sql.ErrNoRows) {
		return Feedback{}, fmt.Errorf("profile %q 未找到条目 %q；请先运行一次不带 -dry-run 的 once", profileID, itemID)
	}
	if err != nil {
		return Feedback{}, err
	}

	feedback.TargetValue = itemID
	if action == FeedbackBlockFeed {
		if strings.TrimSpace(feedURL) == "" {
			return Feedback{}, fmt.Errorf("条目 %q 没有可屏蔽的订阅源", itemID)
		}
		feedback.TargetValue = feedURL
	}
	if action == FeedbackLike {
		if _, err := db.sql.ExecContext(ctx, `DELETE FROM profile_feedback WHERE profile_id = ? AND item_id = ? AND action = ?`, profileID, itemID, FeedbackDislike); err != nil {
			return Feedback{}, err
		}
	}
	if action == FeedbackDislike {
		if _, err := db.sql.ExecContext(ctx, `DELETE FROM profile_feedback WHERE profile_id = ? AND item_id = ? AND action = ?`, profileID, itemID, FeedbackLike); err != nil {
			return Feedback{}, err
		}
	}

	feedback.CreatedAt = time.Now().UTC()
	if isPreferenceSignal(action) {
		_, err = db.sql.ExecContext(ctx, `INSERT INTO profile_feedback_signals (profile_id, item_id, action, created_at)
			VALUES (?, ?, ?, ?) ON CONFLICT(profile_id, item_id, action) DO UPDATE SET created_at = excluded.created_at`,
			profileID, itemID, action, feedback.CreatedAt.Format(time.RFC3339Nano))
		return feedback, err
	}
	_, err = db.sql.ExecContext(ctx, `INSERT INTO profile_feedback (profile_id, item_id, action, target_value, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(profile_id, item_id, action) DO UPDATE SET
			target_value = excluded.target_value,
			created_at = excluded.created_at`,
		profileID,
		feedback.ItemID,
		feedback.Action,
		feedback.TargetValue,
		feedback.CreatedAt.Format(time.RFC3339Nano),
	)
	return feedback, err
}

func (db *DB) RemoveFeedback(ctx context.Context, itemID string, action FeedbackAction) (bool, error) {
	if !action.Valid() {
		return false, fmt.Errorf("不支持的反馈操作 %q", action)
	}
	result, err := db.sql.ExecContext(ctx, `DELETE FROM item_feedback WHERE item_id = ? AND action = ?`, itemID, action)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

func (db *DB) RemoveFeedbackForProfile(ctx context.Context, profileID string, itemID string, action FeedbackAction) (bool, error) {
	if !action.Valid() {
		return false, fmt.Errorf("不支持的反馈操作 %q", action)
	}
	table := "profile_feedback"
	if isPreferenceSignal(action) {
		table = "profile_feedback_signals"
	}
	result, err := db.sql.ExecContext(ctx, `DELETE FROM `+table+` WHERE profile_id = ? AND item_id = ? AND action = ?`, normalizeProfileID(profileID), itemID, action)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

func (db *DB) ListFeedback(ctx context.Context, action FeedbackAction) ([]Feedback, error) {
	if action != "" && !action.Valid() {
		return nil, fmt.Errorf("不支持的反馈操作 %q", action)
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT f.item_id, f.action, f.target_value, f.created_at,
		COALESCE(i.title, ''), COALESCE(i.link, ''), COALESCE(i.feed_name, '')
		FROM item_feedback f
		LEFT JOIN items i ON i.id = f.item_id
		WHERE (? = '' OR f.action = ?)
		ORDER BY f.created_at DESC`, action, action)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	feedback := []Feedback{}
	for rows.Next() {
		var (
			entry      Feedback
			createdRaw string
		)
		if err := rows.Scan(&entry.ItemID, &entry.Action, &entry.TargetValue, &createdRaw, &entry.Title, &entry.Link, &entry.FeedName); err != nil {
			return nil, err
		}
		entry.CreatedAt = parseTime(createdRaw)
		feedback = append(feedback, entry)
	}
	return feedback, rows.Err()
}

func (db *DB) ListFeedbackForProfile(ctx context.Context, profileID string, action FeedbackAction) ([]Feedback, error) {
	if action != "" && !action.Valid() {
		return nil, fmt.Errorf("不支持的反馈操作 %q", action)
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT f.item_id, f.action, f.target_value, f.created_at,
		COALESCE(i.title, ''), COALESCE(i.link, ''), COALESCE(i.feed_name, '')
		FROM profile_feedback f
		LEFT JOIN items i ON i.id = f.item_id
		WHERE f.profile_id = ? AND (? = '' OR f.action = ?)
		ORDER BY f.created_at DESC`, normalizeProfileID(profileID), action, action)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	feedback := []Feedback{}
	for rows.Next() {
		var (
			entry      Feedback
			createdRaw string
		)
		if err := rows.Scan(&entry.ItemID, &entry.Action, &entry.TargetValue, &createdRaw, &entry.Title, &entry.Link, &entry.FeedName); err != nil {
			return nil, err
		}
		entry.CreatedAt = parseTime(createdRaw)
		feedback = append(feedback, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	signalRows, err := db.sql.QueryContext(ctx, `SELECT f.item_id, f.action, f.created_at,
		COALESCE(i.title, ''), COALESCE(i.link, ''), COALESCE(i.feed_name, '')
		FROM profile_feedback_signals f LEFT JOIN items i ON i.id = f.item_id
		WHERE f.profile_id = ? AND (? = '' OR f.action = ?) ORDER BY f.created_at DESC`, normalizeProfileID(profileID), action, action)
	if err != nil {
		return nil, err
	}
	defer signalRows.Close()
	for signalRows.Next() {
		var entry Feedback
		var createdRaw string
		if err := signalRows.Scan(&entry.ItemID, &entry.Action, &createdRaw, &entry.Title, &entry.Link, &entry.FeedName); err != nil {
			return nil, err
		}
		entry.TargetValue = entry.ItemID
		entry.CreatedAt = parseTime(createdRaw)
		feedback = append(feedback, entry)
	}
	return feedback, signalRows.Err()
}

func isPreferenceSignal(action FeedbackAction) bool {
	switch action {
	case FeedbackTooShallow, FeedbackTooTheoretical, FeedbackTooMarketing, FeedbackUnusable, FeedbackMoreLikeThis:
		return true
	default:
		return false
	}
}

func (db *DB) FeedbackFilters(ctx context.Context) (FeedbackFilters, error) {
	filters := FeedbackFilters{
		BlockedItemIDs:  map[string]bool{},
		BlockedFeedURLs: map[string]bool{},
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT item_id, action, target_value FROM item_feedback
		WHERE action IN (?, ?, ?)`, FeedbackDislike, FeedbackBlock, FeedbackBlockFeed)
	if err != nil {
		return FeedbackFilters{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			itemID string
			action FeedbackAction
			target string
		)
		if err := rows.Scan(&itemID, &action, &target); err != nil {
			return FeedbackFilters{}, err
		}
		switch action {
		case FeedbackDislike, FeedbackBlock:
			filters.BlockedItemIDs[itemID] = true
		case FeedbackBlockFeed:
			filters.BlockedFeedURLs[target] = true
		}
	}
	return filters, rows.Err()
}

func (db *DB) FeedbackFiltersForProfile(ctx context.Context, profileID string) (FeedbackFilters, error) {
	filters := FeedbackFilters{
		BlockedItemIDs:  map[string]bool{},
		BlockedFeedURLs: map[string]bool{},
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT item_id, action, target_value FROM profile_feedback
		WHERE profile_id = ? AND action IN (?, ?, ?)`, normalizeProfileID(profileID), FeedbackDislike, FeedbackBlock, FeedbackBlockFeed)
	if err != nil {
		return FeedbackFilters{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			itemID string
			action FeedbackAction
			target string
		)
		if err := rows.Scan(&itemID, &action, &target); err != nil {
			return FeedbackFilters{}, err
		}
		switch action {
		case FeedbackDislike, FeedbackBlock:
			filters.BlockedItemIDs[itemID] = true
		case FeedbackBlockFeed:
			filters.BlockedFeedURLs[target] = true
		}
	}
	return filters, rows.Err()
}

func (db *DB) RecentItems(ctx context.Context, limit int) ([]RecentItem, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT id, title, link, feed_name, published_at
		FROM items ORDER BY updated_db_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []RecentItem{}
	for rows.Next() {
		var (
			item         RecentItem
			publishedRaw string
		)
		if err := rows.Scan(&item.ID, &item.Title, &item.Link, &item.FeedName, &publishedRaw); err != nil {
			return nil, err
		}
		item.PublishedAt = parseTime(publishedRaw)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (db *DB) RecentItemsForProfile(ctx context.Context, profileID string, limit int) ([]RecentItem, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT i.id, i.title, i.link, i.feed_name, i.published_at
		FROM profile_items p JOIN items i ON i.id = p.item_id
		WHERE p.profile_id = ? ORDER BY p.added_at DESC LIMIT ?`, normalizeProfileID(profileID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []RecentItem{}
	for rows.Next() {
		var (
			item         RecentItem
			publishedRaw string
		)
		if err := rows.Scan(&item.ID, &item.Title, &item.Link, &item.FeedName, &publishedRaw); err != nil {
			return nil, err
		}
		item.PublishedAt = parseTime(publishedRaw)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (db *DB) DigestItemsForProfile(ctx context.Context, profileID string, profileHash string, limit int) ([]DigestItem, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 5000 {
		limit = 5000
	}
	profileID = normalizeProfileID(profileID)
	rows, err := db.sql.QueryContext(ctx, `SELECT
		i.id, i.feed_name, i.feed_url, COALESCE(i.title, ''), COALESCE(i.link, ''), COALESCE(i.author, ''),
		COALESCE(i.published_at, ''), COALESCE(i.summary, ''), COALESCE(i.content, ''),
		COALESCE(a.model_label, ''), COALESCE(a.model_name, ''), COALESCE(a.score, 0),
		COALESCE(a.should_push, 0), COALESCE(a.title, ''), COALESCE(a.summary, ''), COALESCE(a.why, ''),
		COALESCE(a.key_points_json, ''), COALESCE(a.tags_json, ''), COALESCE(a.updated_at, ''),
		COALESCE(s.seen_at, ''), COALESCE(s.pushed, 0)
		FROM profile_items p
		JOIN items i ON i.id = p.item_id
		LEFT JOIN item_analyses a ON a.item_id = i.id AND a.profile_hash = ? AND a.content_hash = i.content_hash
		LEFT JOIN profile_seen_items s ON s.profile_id = p.profile_id AND s.item_id = p.item_id
		WHERE p.profile_id = ?
		ORDER BY CASE WHEN a.item_id IS NULL THEN 1 ELSE 0 END, a.score DESC, p.added_at DESC
		LIMIT ?`, profileHash, profileID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []DigestItem{}
	for rows.Next() {
		var (
			item          DigestItem
			publishedRaw  string
			keyPointsJSON string
			tagsJSON      string
			analyzedRaw   string
			seenRaw       string
			shouldPush    int
			pushed        int
		)
		if err := rows.Scan(
			&item.ID,
			&item.FeedName,
			&item.FeedURL,
			&item.Title,
			&item.Link,
			&item.Author,
			&publishedRaw,
			&item.SourceSummary,
			&item.Content,
			&item.ModelLabel,
			&item.ModelName,
			&item.Score,
			&shouldPush,
			&item.AnalysisTitle,
			&item.Summary,
			&item.Why,
			&keyPointsJSON,
			&tagsJSON,
			&analyzedRaw,
			&seenRaw,
			&pushed,
		); err != nil {
			return nil, err
		}
		if keyPointsJSON != "" && json.Unmarshal([]byte(keyPointsJSON), &item.KeyPoints) != nil {
			return nil, fmt.Errorf("decode key points for item %q", item.ID)
		}
		if tagsJSON != "" && json.Unmarshal([]byte(tagsJSON), &item.Tags) != nil {
			return nil, fmt.Errorf("decode tags for item %q", item.ID)
		}
		item.PublishedAt = parseTime(publishedRaw)
		item.AnalyzedAt = parseTime(analyzedRaw)
		item.Seen = seenRaw != ""
		item.Pushed = pushed != 0
		item.ShouldPush = shouldPush != 0
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	feedback, err := db.ListFeedbackForProfile(ctx, profileID, "")
	if err != nil {
		return nil, err
	}
	byItem := make(map[string][]FeedbackAction, len(feedback))
	for _, entry := range feedback {
		byItem[entry.ItemID] = append(byItem[entry.ItemID], entry.Action)
	}
	for i := range items {
		items[i].Feedback = byItem[items[i].ID]
	}
	return items, nil
}

func (db *DB) DigestItemsByIDsForProfile(ctx context.Context, profileID, profileHash string, ids []string) ([]DigestItem, error) {
	if len(ids) == 0 {
		return []DigestItem{}, nil
	}
	all, err := db.DigestItemsForProfile(ctx, profileID, profileHash, 5000)
	if err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	out := make([]DigestItem, 0, len(ids))
	for _, item := range all {
		if wanted[item.ID] {
			out = append(out, item)
		}
	}
	return out, nil
}

func (db *DB) EnqueueAnalysis(ctx context.Context, runID int64, profileID, profileHash, promptVersion string, item rss.Item, priority int) (bool, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var runValue any
	if runID > 0 {
		runValue = runID
	}
	result, err := db.sql.ExecContext(ctx, `INSERT INTO analysis_tasks
		(run_id, profile_id, profile_hash, item_id, content_hash, prompt_version, priority, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?)
		ON CONFLICT(item_id, profile_hash, content_hash, prompt_version) DO UPDATE SET
			priority = MAX(priority, excluded.priority),
			run_id = excluded.run_id,
			status = CASE WHEN analysis_tasks.status IN ('completed', 'running') THEN analysis_tasks.status ELSE 'pending' END,
			next_retry_at = CASE WHEN analysis_tasks.status = 'retry_wait' THEN NULL ELSE analysis_tasks.next_retry_at END,
			updated_at = excluded.updated_at`,
		runValue, normalizeProfileID(profileID), profileHash, item.StableID(), item.ContentHash(), promptVersion, priority, now, now)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

func (db *DB) PromoteAnalysis(ctx context.Context, profileID, profileHash, itemID string, priority int) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE analysis_tasks SET priority = MAX(priority, ?),
		status = CASE WHEN status = 'retry_wait' THEN 'pending' ELSE status END,
		next_retry_at = CASE WHEN status = 'retry_wait' THEN NULL ELSE next_retry_at END,
		updated_at = ? WHERE profile_id = ? AND profile_hash = ? AND item_id = ? AND status NOT IN ('completed', 'failed')`,
		priority, time.Now().UTC().Format(time.RFC3339Nano), normalizeProfileID(profileID), profileHash, itemID)
	return err
}

func (db *DB) RecoverAnalysisTasks(ctx context.Context, staleBefore time.Time) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE analysis_tasks SET status = 'pending', updated_at = ?
		WHERE status = 'running' AND updated_at < ?`, time.Now().UTC().Format(time.RFC3339Nano), staleBefore.UTC().Format(time.RFC3339Nano))
	return err
}

func (db *DB) ClaimAnalysisTasks(ctx context.Context, profileID string, limit int) ([]AnalysisTask, error) {
	if limit <= 0 {
		limit = 8
	}
	now := time.Now().UTC()
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id, COALESCE(run_id,0), profile_id, profile_hash, item_id, content_hash, priority, status, attempts,
		COALESCE(next_retry_at, ''), COALESCE(last_error, '') FROM analysis_tasks
		WHERE profile_id = ? AND (status = 'pending' OR (status = 'retry_wait' AND next_retry_at <= ?))
		ORDER BY priority DESC, created_at ASC LIMIT ?`, normalizeProfileID(profileID), now.Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, err
	}
	var tasks []AnalysisTask
	for rows.Next() {
		var task AnalysisTask
		var retry string
		if err := rows.Scan(&task.ID, &task.RunID, &task.ProfileID, &task.ProfileHash, &task.ItemID, &task.ContentHash, &task.Priority, &task.Status, &task.Attempts, &retry, &task.LastError); err != nil {
			rows.Close()
			return nil, err
		}
		task.NextRetryAt = parseTime(retry)
		tasks = append(tasks, task)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, task := range tasks {
		if _, err := tx.ExecContext(ctx, `UPDATE analysis_tasks SET status = 'running', updated_at = ? WHERE id = ?`, now.Format(time.RFC3339Nano), task.ID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (db *DB) CompleteAnalysisTasks(ctx context.Context, ids []int64) error {
	return db.setTaskState(ctx, ids, "completed", "", time.Time{}, false)
}

func (db *DB) CompleteAnalysisItems(ctx context.Context, profileHash string, items []rss.Item) error {
	for _, item := range items {
		if _, err := db.sql.ExecContext(ctx, `UPDATE analysis_tasks SET status = 'completed', last_error = '', next_retry_at = NULL, updated_at = ?
			WHERE profile_hash = ? AND item_id = ? AND content_hash = ?`, time.Now().UTC().Format(time.RFC3339Nano), profileHash, item.StableID(), item.ContentHash()); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) RetryAnalysisTasks(ctx context.Context, ids []int64, message string, retryAt time.Time, countAttempt bool) error {
	return db.setTaskState(ctx, ids, "retry_wait", message, retryAt, countAttempt)
}

func (db *DB) FailAnalysisTasks(ctx context.Context, ids []int64, message string) error {
	return db.setTaskState(ctx, ids, "failed", message, time.Time{}, true)
}

func (db *DB) setTaskState(ctx context.Context, ids []int64, status, message string, retryAt time.Time, countAttempt bool) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	query := `UPDATE analysis_tasks SET status = ?, last_error = ?, next_retry_at = ?, updated_at = ?, attempts = attempts + ? WHERE id IN (` + placeholders + `)`
	retry := ""
	if !retryAt.IsZero() {
		retry = retryAt.UTC().Format(time.RFC3339Nano)
	}
	args := []any{status, message, retry, time.Now().UTC().Format(time.RFC3339Nano), boolInt(countAttempt)}
	for _, id := range ids {
		args = append(args, id)
	}
	_, err := db.sql.ExecContext(ctx, query, args...)
	return err
}

func (db *DB) AnalysisQueueStats(ctx context.Context, profileID string) (AnalysisQueueStats, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT status,CASE WHEN lower(COALESCE(last_error,'')) LIKE '%429%' OR lower(COALESCE(last_error,'')) LIKE '%rate limit%' OR lower(COALESCE(last_error,'')) LIKE '%too many requests%' THEN 1 ELSE 0 END AS limited,COUNT(*) FROM analysis_tasks WHERE profile_id = ? GROUP BY status,limited`, normalizeProfileID(profileID))
	if err != nil {
		return AnalysisQueueStats{}, err
	}
	defer rows.Close()
	var stats AnalysisQueueStats
	for rows.Next() {
		var status string
		var count, limited int
		if err := rows.Scan(&status, &limited, &count); err != nil {
			return stats, err
		}
		switch status {
		case "completed":
			stats.Completed = count
		case "pending":
			stats.Pending = count
		case "running":
			stats.Running = count
		case "retry_wait":
			if limited != 0 {
				stats.RateLimited += count
			} else {
				stats.Retrying += count
			}
		case "failed":
			stats.Failed = count
		}
	}
	return stats, rows.Err()
}

func (db *DB) CreateAnalysisRun(ctx context.Context, profileID, status string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := db.sql.ExecContext(ctx, `INSERT INTO analysis_runs(profile_id,status,created_at,updated_at) VALUES(?,?,?,?)`, normalizeProfileID(profileID), status, now, now)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (db *DB) SetAnalysisRunTotals(ctx context.Context, runID int64, total, cached int, status string) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE analysis_runs SET total=?, cached=?, status=?, updated_at=? WHERE id=?`, total, cached, status, time.Now().UTC().Format(time.RFC3339Nano), runID)
	return err
}

func (db *DB) SetAnalysisRunStatus(ctx context.Context, runID int64, status string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	completed := any(nil)
	if status == "completed" || status == "partial_failed" {
		completed = now
	}
	_, err := db.sql.ExecContext(ctx, `UPDATE analysis_runs SET status=?, completed_at=COALESCE(?,completed_at), updated_at=? WHERE id=?`, status, completed, now, runID)
	return err
}

func (db *DB) EnsureRecoveryRun(ctx context.Context, profileID string) (int64, error) {
	profileID = normalizeProfileID(profileID)
	if _, err := db.sql.ExecContext(ctx, `UPDATE analysis_tasks SET status='pending', updated_at=? WHERE profile_id=? AND status='running'`, time.Now().UTC().Format(time.RFC3339Nano), profileID); err != nil {
		return 0, err
	}
	if _, err := db.sql.ExecContext(ctx, `UPDATE analysis_tasks SET status='pending',attempts=0,next_retry_at=NULL,updated_at=? WHERE profile_id=? AND status='failed' AND last_error LIKE '%score%0-10%'`, time.Now().UTC().Format(time.RFC3339Nano), profileID); err != nil {
		return 0, err
	}
	var count int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM analysis_tasks WHERE profile_id=? AND run_id IS NULL`, profileID).Scan(&count); err != nil || count == 0 {
		return 0, err
	}
	runID, err := db.CreateAnalysisRun(ctx, profileID, "background")
	if err != nil {
		return 0, err
	}
	if _, err := db.sql.ExecContext(ctx, `UPDATE analysis_tasks SET run_id=? WHERE profile_id=? AND run_id IS NULL`, runID, profileID); err != nil {
		return 0, err
	}
	if err := db.SetAnalysisRunTotals(ctx, runID, count, 0, "background"); err != nil {
		return 0, err
	}
	return runID, nil
}

func (db *DB) CurrentAnalysisRun(ctx context.Context, profileID string) (AnalysisRun, bool, error) {
	var run AnalysisRun
	var created, completed string
	err := db.sql.QueryRowContext(ctx, `SELECT id,profile_id,status,total,cached,created_at,COALESCE(completed_at,'') FROM analysis_runs WHERE profile_id=? ORDER BY CASE WHEN status IN ('initial','background','rate_limited') THEN 0 ELSE 1 END, created_at DESC LIMIT 1`, normalizeProfileID(profileID)).Scan(&run.ID, &run.ProfileID, &run.Status, &run.Total, &run.Cached, &created, &completed)
	if errors.Is(err, sql.ErrNoRows) {
		return run, false, nil
	}
	if err != nil {
		return run, false, err
	}
	run.CreatedAt = parseTime(created)
	run.CompletedAt = parseTime(completed)
	rows, err := db.sql.QueryContext(ctx, `SELECT status,CASE WHEN lower(COALESCE(last_error,'')) LIKE '%429%' OR lower(COALESCE(last_error,'')) LIKE '%rate limit%' OR lower(COALESCE(last_error,'')) LIKE '%too many requests%' THEN 1 ELSE 0 END AS limited,COUNT(*) FROM analysis_tasks WHERE run_id=? GROUP BY status,limited`, run.ID)
	if err != nil {
		return run, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var limited, count int
		if err := rows.Scan(&status, &limited, &count); err != nil {
			return run, false, err
		}
		switch status {
		case "completed":
			run.Analyzed += count
		case "pending":
			run.Pending += count
		case "running":
			run.Running += count
		case "retry_wait":
			if limited != 0 {
				run.RateLimited += count
			} else {
				run.Retrying += count
			}
		case "failed":
			run.Failed += count
		}
	}
	run.Analyzed += run.Cached
	active := run.Pending + run.Running + run.Retrying + run.RateLimited
	if active == 0 && run.Status != "completed" && run.Status != "partial_failed" {
		if run.Failed > 0 {
			run.Status = "partial_failed"
		} else {
			run.Status = "completed"
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		_, _ = db.sql.ExecContext(ctx, `UPDATE analysis_runs SET status=?,completed_at=?,updated_at=? WHERE id=?`, run.Status, now, now, run.ID)
		run.CompletedAt = time.Now().UTC()
	} else if run.RateLimited > 0 {
		run.Status = "rate_limited"
	}
	return run, true, rows.Err()
}

func (db *DB) RecordCostEvent(ctx context.Context, event CostEvent) error {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	_, err := db.sql.ExecContext(ctx, `INSERT INTO cost_events
		(scope, provider, model, model_label, kind, input_tokens, output_tokens, cost_cny, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.Scope,
		event.Provider,
		event.Model,
		event.ModelLabel,
		event.Kind,
		event.InputTokens,
		event.OutputTokens,
		event.CostCNY,
		event.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (db *DB) CostSince(ctx context.Context, scope string, since time.Time) (float64, error) {
	var total sql.NullFloat64
	err := db.sql.QueryRowContext(ctx, `SELECT SUM(cost_cny) FROM cost_events WHERE scope = ? AND created_at >= ?`,
		scope, since.UTC().Format(time.RFC3339Nano)).Scan(&total)
	if err != nil {
		return 0, err
	}
	if !total.Valid {
		return 0, nil
	}
	return total.Float64, nil
}

func (db *DB) TotalCostSince(ctx context.Context, since time.Time) (float64, error) {
	var total sql.NullFloat64
	err := db.sql.QueryRowContext(ctx, `SELECT SUM(cost_cny) FROM cost_events WHERE created_at >= ?`,
		since.UTC().Format(time.RFC3339Nano)).Scan(&total)
	if err != nil {
		return 0, err
	}
	if !total.Valid {
		return 0, nil
	}
	return total.Float64, nil
}

func (db *DB) TokensSince(ctx context.Context, provider string, model string, since time.Time) (int, error) {
	var total sql.NullInt64
	err := db.sql.QueryRowContext(ctx, `SELECT SUM(input_tokens + output_tokens) FROM cost_events
		WHERE provider = ? AND model = ? AND created_at >= ?`,
		provider, model, since.UTC().Format(time.RFC3339Nano)).Scan(&total)
	if err != nil {
		return 0, err
	}
	if !total.Valid {
		return 0, nil
	}
	return int(total.Int64), nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

func normalizeProfileID(profileID string) string {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return defaultProfileID
	}
	return profileID
}

func debugSQL(err error, query string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", query, err)
}
