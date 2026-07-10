package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	FeedbackLike      FeedbackAction = "like"
	FeedbackDislike   FeedbackAction = "dislike"
	FeedbackBlock     FeedbackAction = "block"
	FeedbackSave      FeedbackAction = "save"
	FeedbackLater     FeedbackAction = "later"
	FeedbackBlockFeed FeedbackAction = "block-feed"
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

func ParseFeedbackAction(raw string) (FeedbackAction, error) {
	action := FeedbackAction(strings.ToLower(strings.TrimSpace(raw)))
	if !action.Valid() {
		return "", fmt.Errorf("不支持的反馈操作 %q", raw)
	}
	return action, nil
}

func (a FeedbackAction) Valid() bool {
	switch a {
	case FeedbackLike, FeedbackDislike, FeedbackBlock, FeedbackSave, FeedbackLater, FeedbackBlockFeed:
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
	result, err := db.sql.ExecContext(ctx, `DELETE FROM profile_feedback WHERE profile_id = ? AND item_id = ? AND action = ?`, normalizeProfileID(profileID), itemID, action)
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
	return feedback, rows.Err()
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
