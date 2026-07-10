package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jeffery/rss-agent/internal/agent"
	"github.com/jeffery/rss-agent/internal/rss"
	_ "modernc.org/sqlite"
)

type DB struct {
	sql *sql.DB
}

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
		`CREATE TABLE IF NOT EXISTS seen_items (
			item_id TEXT PRIMARY KEY,
			title TEXT,
			link TEXT,
			feed_name TEXT,
			seen_at TEXT NOT NULL,
			pushed INTEGER NOT NULL DEFAULT 0
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
	}
	for _, stmt := range stmts {
		if _, err := db.sql.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) GetFeedState(ctx context.Context, feedURL string) (rss.FeedFetchState, bool, error) {
	row := db.sql.QueryRowContext(ctx, `SELECT feed_url, etag, last_modified, last_status, last_error, fail_count, last_fetched_at
		FROM feed_fetch_states WHERE feed_url = ?`, feedURL)
	var state rss.FeedFetchState
	var fetchedAt string
	if err := row.Scan(&state.FeedURL, &state.ETag, &state.LastModified, &state.LastStatus, &state.LastError, &state.FailCount, &fetchedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return rss.FeedFetchState{}, false, nil
		}
		return rss.FeedFetchState{}, false, err
	}
	state.LastFetchedAt = parseTime(fetchedAt)
	return state, true, nil
}

func (db *DB) SaveFeedState(ctx context.Context, state rss.FeedFetchState) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.sql.ExecContext(ctx, `INSERT INTO feed_fetch_states
		(feed_url, etag, last_modified, last_status, last_error, fail_count, last_fetched_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(feed_url) DO UPDATE SET
			etag = excluded.etag,
			last_modified = excluded.last_modified,
			last_status = excluded.last_status,
			last_error = excluded.last_error,
			fail_count = excluded.fail_count,
			last_fetched_at = excluded.last_fetched_at,
			updated_at = excluded.updated_at`,
		state.FeedURL,
		state.ETag,
		state.LastModified,
		state.LastStatus,
		state.LastError,
		state.FailCount,
		formatTime(state.LastFetchedAt),
		now,
	)
	return err
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

func (db *DB) CachedAnalysis(ctx context.Context, item rss.Item, profileHash string, ttl time.Duration) (agent.Result, bool, error) {
	row := db.sql.QueryRowContext(ctx, `SELECT model_label, score, should_push, title, summary, why, key_points_json, tags_json, created_at
		FROM item_analyses WHERE item_id = ? AND profile_hash = ? AND content_hash = ?`,
		item.StableID(), profileHash, item.ContentHash())
	var (
		modelLabel    string
		decision      agent.Decision
		shouldPush    int
		keyPointsJSON string
		tagsJSON      string
		createdAtRaw  string
	)
	if err := row.Scan(&modelLabel, &decision.Score, &shouldPush, &decision.Title, &decision.Summary, &decision.Why, &keyPointsJSON, &tagsJSON, &createdAtRaw); err != nil {
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
	return agent.Result{Item: item, Decision: decision, ModelLabel: modelLabel, Cached: true}, true, nil
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

func debugSQL(err error, query string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", query, err)
}
