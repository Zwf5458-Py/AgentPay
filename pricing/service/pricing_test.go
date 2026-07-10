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

func TestPricingService_RecordUsageBuffer_Fallback(t *testing.T) {
	svc := newTestService(t)

	// 阶梯折扣配置
	cfg := &model.PriceConfig{
		AgentID:        "agent-buffer-test",
		ModelCostPer1k: 1500,
		ServiceFee:     2000,
		PlatformBps:    1000,
		TierThresholds: []uint64{5000, 10000},
		TierDiscounts:  []float64{0.9, 0.8},
	}
	if err := svc.store.SavePriceConfig(context.Background(), cfg); err != nil {
		t.Fatalf("save price config: %v", err)
	}

	// 1. 发起高频调用暂存
	q := &model.Quote{ModelCost: 1500, Model: model.Tiered}
	err := svc.RecordUsage(context.Background(), "agent-buffer-test", "call-1", 3000, q)
	if err != nil {
		t.Fatalf("record usage 1 failed: %v", err)
	}
	err2 := svc.RecordUsage(context.Background(), "agent-buffer-test", "call-2", 4000, q)
	if err2 != nil {
		t.Fatalf("record usage 2 failed: %v", err2)
	}

	// 此时 SQLite DB 中总 tokens 应该还是 0（因为还没有刷盘）
	dbTokens, _ := svc.store.GetCumulativeTokens(context.Background(), "agent-buffer-test")
	if dbTokens != 0 {
		t.Errorf("dbTokens = %d, want 0 (delayed flush not working)", dbTokens)
	}

	// 2. 检查阶梯一致性防线：当前 Quote 能否正确感知到 3000 + 4000 = 7000 未落库 tokens，从而命中 5000 以上的 9 折折扣
	qBig, errQuote := svc.Quote(context.Background(), "agent-buffer-test", 1000, "")
	if errQuote != nil {
		t.Fatalf("quote: %v", errQuote)
	}
	// 无折扣 1500，9 折后为 1350
	if qBig.ModelCost != 1350 {
		t.Errorf("qBig.ModelCost = %d, want 1350 (should apply 0.9 discount)", qBig.ModelCost)
	}

	// 3. 执行 Flush 刷盘
	svc.FlushUsage(context.Background())

	// 刷盘后，SQLite 中的累计 tokens 应完成入库，计数器应扣减
	dbTokensAfter, _ := svc.store.GetCumulativeTokens(context.Background(), "agent-buffer-test")
	if dbTokensAfter != 7000 {
		t.Errorf("dbTokensAfter = %d, want 7000 (flush failed)", dbTokensAfter)
	}

	// 缓存 tokens 应由于刷盘完成扣减清零
	cachedTokens := svc.getCachedTokens(context.Background(), "agent-buffer-test")
	if cachedTokens != 0 {
		t.Errorf("cachedTokens = %d, want 0 after flush", cachedTokens)
	}
}
