package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"pricing/model"
)

// SQLiteStore 定价持久层 SQLite 实现。
// 复用 ledger 的 WAL + 单连接模式：SetMaxOpenConns(1) 杜绝并发文件锁，
// PRAGMA journal_mode=WAL + busy_timeout 提高并发吞吐。
type SQLiteStore struct {
	db  *sql.DB
	mu  sync.Mutex // 串行化写，确保幂等与累计读一致
}

func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	store := &SQLiteStore{db: db}
	if err := store.bootstrap(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) bootstrap() error {
	schema := []string{
		`CREATE TABLE IF NOT EXISTS price_configs (
			agent_id TEXT PRIMARY KEY,
			model_cost_per_1k INTEGER NOT NULL,
			service_fee INTEGER NOT NULL,
			platform_bps INTEGER NOT NULL,
			tier_thresholds TEXT NOT NULL DEFAULT '',
			tier_discounts TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS subscriptions (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL UNIQUE,
			plan TEXT NOT NULL,
			quota_calls INTEGER NOT NULL,
			used_calls INTEGER NOT NULL DEFAULT 0,
			expires_at INTEGER NOT NULL,
			created_at DATETIME NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS usage_records (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			call_id TEXT NOT NULL,
			tokens INTEGER NOT NULL,
			cost INTEGER NOT NULL,
			model TEXT NOT NULL,
			timestamp DATETIME NOT NULL
		);`,
	}
	for _, ddl := range schema {
		if _, err := s.db.Exec(ddl); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) SavePriceConfig(ctx context.Context, cfg *model.PriceConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	threshJSON, _ := json.Marshal(cfg.TierThresholds)
	discJSON, _ := json.Marshal(cfg.TierDiscounts)
	_, err := s.db.Exec(`
		INSERT INTO price_configs (agent_id, model_cost_per_1k, service_fee, platform_bps, tier_thresholds, tier_discounts)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			model_cost_per_1k = excluded.model_cost_per_1k,
			service_fee = excluded.service_fee,
			platform_bps = excluded.platform_bps,
			tier_thresholds = excluded.tier_thresholds,
			tier_discounts = excluded.tier_discounts
	`, cfg.AgentID, cfg.ModelCostPer1k, cfg.ServiceFee, cfg.PlatformBps, string(threshJSON), string(discJSON))
	return err
}

func (s *SQLiteStore) GetPriceConfig(ctx context.Context, agentID string) (*model.PriceConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRow(`
		SELECT agent_id, model_cost_per_1k, service_fee, platform_bps, tier_thresholds, tier_discounts
		FROM price_configs WHERE agent_id = ?`, agentID)
	cfg := &model.PriceConfig{}
	var threshStr, discStr string
	err := row.Scan(&cfg.AgentID, &cfg.ModelCostPer1k, &cfg.ServiceFee, &cfg.PlatformBps, &threshStr, &discStr)
	if threshStr != "" {
		json.Unmarshal([]byte(threshStr), &cfg.TierThresholds)
	}
	if discStr != "" {
		json.Unmarshal([]byte(discStr), &cfg.TierDiscounts)
	}
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func (s *SQLiteStore) SaveSubscription(ctx context.Context, sub *model.Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO subscriptions (id, agent_id, plan, quota_calls, used_calls, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			id = excluded.id, plan = excluded.plan, quota_calls = excluded.quota_calls,
			used_calls = excluded.used_calls, expires_at = excluded.expires_at
	`, sub.ID, sub.AgentID, sub.Plan, sub.QuotaCalls, sub.UsedCalls, sub.ExpiresAt, sub.CreatedAt)
	return err
}

func (s *SQLiteStore) GetSubscription(ctx context.Context, agentID string) (*model.Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRow(`
		SELECT id, agent_id, plan, quota_calls, used_calls, expires_at, created_at
		FROM subscriptions WHERE agent_id = ?`, agentID)
	sub := &model.Subscription{}
	var createdAt time.Time
	err := row.Scan(&sub.ID, &sub.AgentID, &sub.Plan, &sub.QuotaCalls, &sub.UsedCalls, &sub.ExpiresAt, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sub.CreatedAt = createdAt
	return sub, nil
}

func (s *SQLiteStore) IncrementUsedCalls(ctx context.Context, agentID string, by uint64) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var used uint64
	if err := s.db.QueryRow(
		`UPDATE subscriptions SET used_calls = used_calls + ? WHERE agent_id = ? RETURNING used_calls`,
		by, agentID,
	).Scan(&used); err != nil {
		return 0, err
	}
	return used, nil
}

func (s *SQLiteStore) AppendUsage(ctx context.Context, rec *model.UsageRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO usage_records (id, agent_id, call_id, tokens, cost, model, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, rec.ID, rec.AgentID, rec.CallID, rec.Tokens, rec.Cost, string(rec.Model), rec.Timestamp)
	return err
}

func (s *SQLiteStore) GetCumulativeTokens(ctx context.Context, agentID string) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var total sql.NullInt64
	if err := s.db.QueryRow(
		`SELECT COALESCE(SUM(tokens), 0) FROM usage_records WHERE agent_id = ?`, agentID,
	).Scan(&total); err != nil {
		return 0, err
	}
	if !total.Valid {
		return 0, nil
	}
	return uint64(total.Int64), nil
}

func (s *SQLiteStore) AppendUsageBatch(ctx context.Context, records []*model.UsageRecord) error {
	if len(records) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO usage_records (id, agent_id, call_id, tokens, cost, model, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, rec := range records {
		_, err := stmt.ExecContext(ctx, rec.ID, rec.AgentID, rec.CallID, rec.Tokens, rec.Cost, string(rec.Model), rec.Timestamp)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}
