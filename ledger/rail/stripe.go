package rail

import (
	"context"
	"fmt"
	"log"
)

type StripeRail struct {
	isMock bool
}

func NewStripeRail(isMock bool) *StripeRail {
	return &StripeRail{
		isMock: isMock,
	}
}

func (r *StripeRail) Lock(ctx context.Context, payer, agent string, amount uint64) (string, error) {
	// 创建一个 Stripe 锁存会话
	lockID := fmt.Sprintf("st_lock_%s_%d", agent, amount)
	log.Printf("[StripeRail] Locked amount %d for agent %s. LockID: %s", amount, agent, lockID)
	return lockID, nil
}

func (r *StripeRail) Split(ctx context.Context, lockID string, payouts []Payout) error {
	log.Printf("[StripeRail] Splitting payouts for LockID %s:", lockID)
	for _, p := range payouts {
		log.Printf(" - Payout to %s: %d", p.Target, p.Amount)
	}
	return nil
}

func (r *StripeRail) Refund(ctx context.Context, lockID string, reason string) error {
	log.Printf("[StripeRail] Refunding LockID %s, reason: %s", lockID, reason)
	return nil
}

func (r *StripeRail) Verify(ctx context.Context, lockID string) (bool, error) {
	log.Printf("[StripeRail] Verifying LockID %s", lockID)
	return true, nil
}
