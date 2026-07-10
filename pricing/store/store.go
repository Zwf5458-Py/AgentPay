package store

import (
	"context"

	"pricing/model"
)

// PricingStore 定价持久层抽象。dev 用 SQLite（复用 ledger 的 WAL 单连接模式），
// 生产可换 Postgres adapter。
type PricingStore interface {
	// SavePriceConfig 写入/更新某 agent 的计费参数
	SavePriceConfig(ctx context.Context, cfg *model.PriceConfig) error
	// GetPriceConfig 读取计费参数，缺失返回 (nil, nil) 由上层回退默认
	GetPriceConfig(ctx context.Context, agentID string) (*model.PriceConfig, error)

	// SaveSubscription 创建/更新订阅
	SaveSubscription(ctx context.Context, sub *model.Subscription) error
	// GetSubscription 读取有效订阅（调用方自行判断 ExpiresAt）
	GetSubscription(ctx context.Context, agentID string) (*model.Subscription, error)
	// IncrementUsedCalls 原子递增已用次数，返回最新值
	IncrementUsedCalls(ctx context.Context, agentID string, by uint64) (uint64, error)

	// AppendUsage 记录一次调用用量（阶梯累计 + 对账）
	AppendUsage(ctx context.Context, rec *model.UsageRecord) error
	// GetCumulativeTokens 返回某 agent 累计 token（阶梯定价用）
	GetCumulativeTokens(ctx context.Context, agentID string) (uint64, error)
}
