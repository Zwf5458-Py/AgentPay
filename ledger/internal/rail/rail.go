package rail

import (
	"context"
)

// Payout 定义了结算时的收款账户和金额
type Payout struct {
	Target string // 目标账户或钱包地址
	Amount uint64 // 结算金额 (对于 USDC 来说是以微 USDC 即 1e-6 为单位)
}

// PaymentRail 定义了统一的支付渠道操作接口，可插拔适配链上通道与法币渠道
type PaymentRail interface {
	// Lock 冻结/锁存一笔资产。对于 CryptoRail 即验证通道并在通道内留存额度，对于 StripeRail 即创建一个锁定状态的会话
	Lock(ctx context.Context, payer string, agent string, amount uint64) (lockID string, err error)

	// Split 确认结算这笔冻结资产，并按 Payout 列表进行资金分账
	Split(ctx context.Context, lockID string, payouts []Payout) error

	// Refund 原路退回已被冻结但未消费的资产
	Refund(ctx context.Context, lockID string, reason string) error

	// Verify 验证一笔冻结或结算是否存在且有效
	Verify(ctx context.Context, lockID string) (bool, error)
}
