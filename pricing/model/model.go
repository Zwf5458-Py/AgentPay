package model

import "time"

// BillingModel 标识本次计费采用的模型
type BillingModel string

const (
	// PerCall 按次计费：按 token 用量 × 单价线性计费
	PerCall BillingModel = "per_call"
	// Tiered 阶梯定价：按账户累计用量落入不同单价档位
	Tiered BillingModel = "tiered"
	// SubscriptionModel 订阅：先扣订阅配额，超额回退按次计费
	SubscriptionModel BillingModel = "subscription"
)

// PriceConfig 描述某 agent 的计费参数。由 PricingService 从 store 读取，
// 缺失时回退到全局 env 默认值（与 legacy reverse.go 行为一致）。
type PriceConfig struct {
	AgentID        string
	ModelCostPer1k   uint64 // 每 1k token 的微单位成本（默认 1500 → $0.0015/1k）
	ServiceFee     uint64 // 代理服务费（微单位，默认 2000）
	PlatformBps    uint16 // 平台税率基点（默认 1000 = 10%）
	TierThresholds []uint64 // 阶梯阈值（累计 token），升序；空表示不启用阶梯
	TierDiscounts  []float64 // 各档单价折扣系数（0.8 = 八折），与阈值等长+1
}

// Subscription 订阅记录。配额按"调用次数"或"累计 token"计，本实现用调用次数。
type Subscription struct {
	ID         string
	AgentID   string
	Plan       string // "basic" | "pro" | ...
	QuotaCalls uint64 // 周期总配额
	UsedCalls  uint64 // 已用次数
	ExpiresAt  int64  // 周期到期 Unix 秒
	CreatedAt  time.Time
}

// UsageRecord 一次调用的用量记录，用于阶梯累计与对账。
type UsageRecord struct {
	ID        string
	AgentID  string
	CallID    string // 网关请求 ID（幂等键）
	Tokens    uint64
	Cost      uint64 // 本次微单位成本
	Model     BillingModel
	Timestamp time.Time
}

// Quote 计费报价结果。PricingService.Quote 返回后，由调用方
// （reverse.go / mcp pay）据此构造 ledger.Invoice 与 rail.Payouts。
type Quote struct {
	AgentID     string
	Model        BillingModel
	Tokens       uint64
	ModelCost   uint64 // 模型调用成本（微单位）
	ServiceFee   uint64 // 代理服务费（微单位）
	PlatformBps uint16 // 平台税率基点
	MicroAmount  uint64 // 实际冻结/扣款总额（含平台税）
	QuotaUsed   bool   // 是否走订阅配额扣减
	Refundable  bool   // 是否支持订阅退款
}
