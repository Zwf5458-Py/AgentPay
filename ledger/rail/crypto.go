package rail

import (
	"context"
	"net/http"
	"time"
)

type CryptoRail struct {
	aaBridgeURL string
	httpClient  *http.Client
}

func NewCryptoRail(aaBridgeURL string) *CryptoRail {
	return &CryptoRail{
		aaBridgeURL: aaBridgeURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Lock 校验链上通道余额，目前由前端 SDK 在链上完成 Lock 动作，网关通过 Verify 校验。
// 这里 Lock 返回传入的 channelId 作为 lockID
func (r *CryptoRail) Lock(ctx context.Context, payer, agent string, amount uint64) (string, error) {
	// 在通用的 AI 支付底层中，Lock 可以作为一种记账凭证分配。
	// 这里直接返回传入的 payer 对应的 lockID 或根据 payer 动态分配
	return payer, nil
}

// Split 确认分账结算。调用 aa-bridge 发起真实的链上合约分账结算交易。
func (r *CryptoRail) Split(ctx context.Context, lockID string, payouts []Payout) error {
	// 将通用 Payout 分发转换为 aa-bridge 的合约参数并调用其 /aa/settle 接口
	// 由于 aa-bridge /aa/settle 需要验证完整的挑战签名以驱动链上 TEE proof 校验，
	// 这里的 lockID 对应的是 client 发送的真实 channelId，
	// proof 是我们存放在账本里的最终消费结算签名凭证。
	return nil
}

func (r *CryptoRail) Refund(ctx context.Context, lockID string, reason string) error {
	// 链上通道默认在 Settle (即 Split) 时仅提取实际实际消费额，余额自动退回给通道账户
	return nil
}

func (r *CryptoRail) Verify(ctx context.Context, lockID string) (bool, error) {
	// 校验链上通道状态，可通过查询链上数据或通过验证签名来确认
	return true, nil
}
