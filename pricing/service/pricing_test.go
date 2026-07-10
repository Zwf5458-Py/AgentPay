package service

import (
	"context"
	"testing"
	"time"

	"pricing/model"
	"pricing/store"
)

func newTestService(t *testing.T) *PricingService {
	t.Helper()
	st, err := store.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return NewPricingService(st)
}

func TestQuote_PerCall(t *testing.T) {
	svc := newTestService(t)
	q, err := svc.Quote(context.Background(), "agent-1", 2000, "")
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	// 2000 tokens → 2 × 1500 = 3000 micro; +2000 fee; /(10000-1000)*10000
	if q.ModelCost != 3000 {
		t.Errorf("modelCost = %d, want 3000", q.ModelCost)
	}
	if q.Model != model.PerCall {
		t.Errorf("model = %s, want per_call", q.Model)
	}
	if q.MicroAmount == 0 {
		t.Errorf("microAmount should be > 0")
	}
}

func TestQuote_SubscriptionQuota(t *testing.T) {
	svc := newTestService(t)
	st := svc.store
	sub := &model.Subscription{
		ID:        "sub-1",
		AgentID:   "agent-sub",
		Plan:      "pro",
		QuotaCalls: 10,
		UsedCalls: 0,
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	if err := st.SaveSubscription(context.Background(), sub); err != nil {
		t.Fatalf("save sub: %v", err)
	}

	q, err := svc.Quote(context.Background(), "agent-sub", 1000, "")
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if q.Model != model.SubscriptionModel {
		t.Errorf("model = %s, want subscription", q.Model)
	}
	if !q.QuotaUsed {
		t.Errorf("QuotaUsed should be true within quota")
	}
	if !q.Refundable {
		t.Errorf("subscription call should be refundable")
	}

	// 用尽配额后回退 per_call
	for i := 0; i < 10; i++ {
		if _, err := st.IncrementUsedCalls(context.Background(), "agent-sub", 1); err != nil {
			t.Fatalf("incr: %v", err)
		}
	}
	q2, err := svc.Quote(context.Background(), "agent-sub", 1000, "")
	if err != nil {
		t.Fatalf("quote2: %v", err)
	}
	if q2.Model != model.PerCall {
		t.Errorf("after quota exhausted model = %s, want per_call", q2.Model)
	}
	if q2.QuotaUsed {
		t.Errorf("QuotaUsed should be false after quota exhausted")
	}
}

func TestQuote_Tiered(t *testing.T) {
	svc := newTestService(t)
	st := svc.store
	cfg := &model.PriceConfig{
		AgentID:       "agent-tier",
		ModelCostPer1k: 1500,
		ServiceFee:     2000,
		PlatformBps:    1000,
		TierThresholds: []uint64{10000, 50000},
		TierDiscounts:  []float64{0.9, 0.8}, // tier0 0.9, tier1 0.8
	}
	if err := st.SavePriceConfig(context.Background(), cfg); err != nil {
		t.Fatalf("save cfg: %v", err)
	}

	// 小量：落入默认档（无折扣）
	qSmall, _ := svc.Quote(context.Background(), "agent-tier", 1000, "")
	if qSmall.ModelCost != 1500 {
		t.Errorf("small modelCost = %d, want 1500", qSmall.ModelCost)
	}

	// 累计后大量：应触发折扣档
	// 写入累计 60000 tokens 的用量记录
	for i := 0; i < 60; i++ {
		_ = st.AppendUsage(context.Background(), &model.UsageRecord{
			ID: "u-"+string(rune('a'+i)), AgentID: "agent-tier", CallID: "c-" + string(rune('a'+i)),
			Tokens: 1000, Cost: 1500, Model: model.Tiered, Timestamp: time.Now(),
		})
	}
	qBig, _ := svc.Quote(context.Background(), "agent-tier", 1000, "")
	if qBig.ModelCost >= 1500 {
		t.Errorf("tiered big modelCost = %d, want < 1500 (discount applied)", qBig.ModelCost)
	}
	if qBig.Model != model.Tiered {
		t.Errorf("big model = %s, want tiered", qBig.Model)
	}
}

func TestQuote_BillingModelHint(t *testing.T) {
	svc := newTestService(t)
	st := svc.store
	// 写入有效订阅，让 hint=subscription 正常命中（否则 quoteSubscription 回退 per_call）
	sub := &model.Subscription{
		ID:        "sub-hint",
		AgentID:   "agent-x",
		Plan:      "basic",
		QuotaCalls: 5,
		UsedCalls: 0,
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	if err := st.SaveSubscription(context.Background(), sub); err != nil {
		t.Fatalf("save sub: %v", err)
	}
	q, err := svc.Quote(context.Background(), "agent-x", 1000, string(model.SubscriptionModel))
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if q.Model != model.SubscriptionModel {
		t.Errorf("hint subscription should force model = %s, got %s", model.SubscriptionModel, q.Model)
	}
}
