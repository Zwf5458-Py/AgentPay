package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"pricing/model"
	"pricing/store"

	"github.com/redis/go-redis/v9"
)

// 默认计费参数，与 legacy reverse.go 行为一致，可由 PriceConfig 逐 agent 覆盖。
const (
	defaultModelCostPer1k uint64 = 1500 // $0.0015 / 1k tokens
	defaultServiceFee     uint64 = 2000 // $0.002 USDC
	defaultPlatformBps   uint16 = 1000 // 10%
)

// PricingService 通用计费引擎。
// 支持三种模型：per_call（按次/按 token 线性）、tiered（阶梯单价）、
// subscription（订阅配额优先，超额回退 per_call）。
// Quote 是幂等的纯计算——它只报价，不改账；真正的资金动作由 ledger.CreateInvoice 完成。
type PricingService struct {
	store store.PricingStore

	mu            sync.RWMutex
	configCache  map[string]*model.PriceConfig
	subCache     map[string]*model.Subscription
	cacheExpiry map[string]time.Time
	cacheTTL     time.Duration

	// Redis 暂存与内存降级缓冲支持
	redisClient    *redis.Client
	memUsageBuffer chan *model.UsageRecord
	memTokensMap   sync.Map // key: agentID (string) -> tokens (uint64)
}

func NewPricingService(s store.PricingStore) *PricingService {
	return &PricingService{
		store:          s,
		configCache:   make(map[string]*model.PriceConfig),
		subCache:      make(map[string]*model.Subscription),
		cacheExpiry:   make(map[string]time.Time),
		cacheTTL:      30 * time.Second,
		memUsageBuffer: make(chan *model.UsageRecord, 10000),
	}
}

func (p *PricingService) SetRedisClient(client *redis.Client) {
	p.redisClient = client
}

// Quote 根据 agentID + tokens 计算本次报价。
// modelHint 允许调用方强制模型（如 "subscription"），为空时自动选择：
// 有效订阅存在 → subscription；否则若配置了阶梯阈值 → tiered；否则 → per_call。
func (p *PricingService) Quote(ctx context.Context, agentID string, tokens uint64, modelHint string) (*model.Quote, error) {
	if agentID == "" {
		return nil, errors.New("agentID is required for pricing")
	}

	cfg := p.resolveConfig(ctx, agentID)
	sub := p.resolveSubscription(ctx, agentID)

	// 选定计费模型
	bm := model.PerCall
	if modelHint != "" {
		switch modelHint {
		case "subscription":
			bm = model.SubscriptionModel
		case "tiered":
			bm = model.Tiered
		case "per_call":
			bm = model.PerCall
		default:
			bm = model.BillingModel(modelHint)
		}
	} else if sub != nil && sub.ExpiresAt > time.Now().Unix() {
		bm = model.SubscriptionModel
	} else if len(cfg.TierThresholds) > 0 {
		bm = model.Tiered
	}

	if bm == model.SubscriptionModel {
		return p.quoteSubscription(ctx, agentID, tokens, cfg, sub)
	} else if bm == model.Tiered {
		return p.quoteTiered(ctx, agentID, tokens, cfg)
	} else {
		return p.quotePerCall(ctx, agentID, tokens, cfg)
	}
}

// quotePerCall 按次/按 token 线性：modelCost = ceil(tokens/1000) * per1k
func (p *PricingService) quotePerCall(ctx context.Context, agentID string, tokens uint64, cfg *model.PriceConfig) (*model.Quote, error) {
	modelCost := (tokens / 1000) * cfg.ModelCostPer1k
	if tokens%1000 != 0 {
		modelCost += cfg.ModelCostPer1k // 不足 1k 按 1k 计
	}
	q := p.assembleQuote(agentID, model.PerCall, tokens, modelCost, cfg, false)
	return q, nil
}

// quoteTiered 阶梯定价：按累计 token 落入对应档位，单价乘折扣系数。
func (p *PricingService) quoteTiered(ctx context.Context, agentID string, tokens uint64, cfg *model.PriceConfig) (*model.Quote, error) {
	cum, err := p.store.GetCumulativeTokens(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("tiered: get cumulative tokens: %w", err)
	}

	// 累加尚未刷盘的缓存增量
	cum += p.getCachedTokens(ctx, agentID)

	// 找最高不超过 (累计+本次) 的阈值档
	discount := 1.0
	for i, th := range cfg.TierThresholds {
		if (cum + tokens) >= th && i < len(cfg.TierDiscounts) {
			discount = cfg.TierDiscounts[i]
		}
	}

	modelCost := (tokens / 1000) * cfg.ModelCostPer1k
	if tokens%1000 != 0 {
		modelCost += cfg.ModelCostPer1k
	}
	modelCost = uint64(float64(modelCost) * discount)

	q := p.assembleQuote(agentID, model.Tiered, tokens, modelCost, cfg, false)
	return q, nil
}

// quoteSubscription 订阅优先：若配额未用尽，本次计为订阅内（modelCost 仍按量算，
// 但标记 QuotaUsed=true 并递增 used_calls；超额则回退 per_call 且 QuotaUsed=false。
func (p *PricingService) quoteSubscription(ctx context.Context, agentID string, tokens uint64, cfg *model.PriceConfig, sub *model.Subscription) (*model.Quote, error) {
	if sub == nil || sub.ExpiresAt <= time.Now().Unix() {
		// 无有效订阅 → 回退 per_call
		return p.quotePerCall(ctx, agentID, tokens, cfg)
	}

	// 从 store 读取实时的 used_calls（缓存可能因测试直接改 store 而滞后）
	if fresh, ferr := p.store.GetSubscription(ctx, agentID); ferr == nil && fresh != nil {
		sub.UsedCalls = fresh.UsedCalls
	}

	quotaUsed := false
	if sub.UsedCalls < sub.QuotaCalls {
		quotaUsed = true
		if _, err := p.store.IncrementUsedCalls(ctx, agentID, 1); err != nil {
			return nil, fmt.Errorf("subscription: increment used calls: %w", err)
		}
	}
	if !quotaUsed {
		// 配额已用尽，回退按次计费
		return p.quotePerCall(ctx, agentID, tokens, cfg)
	}
	// 配额内：modelCost 按 token 量估算用于展示/冻结上限
	modelCost := (tokens / 1000) * cfg.ModelCostPer1k
	if tokens%1000 != 0 {
		modelCost += cfg.ModelCostPer1k
	}
	q := p.assembleQuote(agentID, model.SubscriptionModel, tokens, modelCost, cfg, true)
	q.Refundable = true // 订阅内调用支持周期退款
	return q, nil
}

// assembleQuote 由 modelCost/serviceFee/platformBps 组装最终报价。
// MicroAmount 含平台税（与 legacy reverse.go 一致：actual = (model+svc)*10000/(10000-bps)）。
func (p *PricingService) assembleQuote(agentID string, m model.BillingModel, tokens, modelCost uint64, cfg *model.PriceConfig, quotaUsed bool) *model.Quote {
	serviceFee := cfg.ServiceFee
	bps := cfg.PlatformBps

	// 实际冻结总额（含平台税），对齐合约 splitSettle 的 accumulatedAmount 口径
	denom := uint64(10000) - uint64(bps)
	var microAmount uint64
	if denom > 0 && (modelCost+serviceFee) > 0 {
		microAmount = (modelCost + serviceFee) * 10000 / denom
	} else {
		microAmount = modelCost + serviceFee
	}

	return &model.Quote{
		AgentID:    agentID,
		Model:       m,
		Tokens:      tokens,
		ModelCost:   modelCost,
		ServiceFee:  serviceFee,
		PlatformBps: bps,
		MicroAmount: microAmount,
		QuotaUsed:   quotaUsed,
		Refundable:  false,
	}
}

// RecordUsage 记账一次调用（幂等键 = callID）。用于阶梯累计与对账。
// 仅在 Quote 已成功、且本次真要计费时调用。
func (p *PricingService) RecordUsage(ctx context.Context, agentID, callID string, tokens uint64, q *model.Quote) error {
	if callID == "" {
		callID = fmt.Sprintf("%s_%d", agentID, time.Now().UnixNano())
	}
	rec := &model.UsageRecord{
		ID:        callID,
		AgentID:  agentID,
		CallID:    callID,
		Tokens:    tokens,
		Cost:      q.ModelCost,
		Model:     q.Model,
		Timestamp: time.Now(),
	}

	// 1. 将用量增量 tokens 累加至暂存缓存（用于 quote 时阶梯计费精确对账）
	p.addCachedTokens(ctx, agentID, tokens)

	// 2. 将明细记录暂存入缓冲队列（优先 Redis，降级为内存 channel）
	if p.redisClient != nil {
		val, err := json.Marshal(rec)
		if err != nil {
			return fmt.Errorf("failed to marshal usage record: %w", err)
		}
		key := "pricing:usage:buffer"
		if err := p.redisClient.RPush(ctx, key, string(val)).Err(); err != nil {
			// Redis 写入失败，退回内存 channel
			select {
			case p.memUsageBuffer <- rec:
			default:
				log.Printf("[PricingService] Warning: Both Redis and Memory channel buffer are full, dropping record %s", callID)
			}
		}
	} else {
		select {
		case p.memUsageBuffer <- rec:
		default:
			log.Printf("[PricingService] Warning: Memory channel buffer is full, dropping record %s", callID)
		}
	}

	return nil
}

func (p *PricingService) addCachedTokens(ctx context.Context, agentID string, tokens uint64) {
	if p.redisClient != nil {
		key := fmt.Sprintf("pricing:accumulated:tokens:%s", agentID)
		_ = p.redisClient.IncrBy(ctx, key, int64(tokens)).Err()
		return
	}

	// 降级模式：内存增量累加
	p.mu.Lock()
	defer p.mu.Unlock()
	var current uint64 = 0
	if val, ok := p.memTokensMap.Load(agentID); ok {
		if num, ok2 := val.(uint64); ok2 {
			current = num
		}
	}
	p.memTokensMap.Store(agentID, current+tokens)
}

func (p *PricingService) getCachedTokens(ctx context.Context, agentID string) uint64 {
	if p.redisClient != nil {
		key := fmt.Sprintf("pricing:accumulated:tokens:%s", agentID)
		val, err := p.redisClient.Get(ctx, key).Result()
		if err == nil && val != "" {
			if num, err := strconv.ParseUint(val, 10, 64); err == nil {
				return num
			}
		}
		return 0
	}

	// 降级模式：从内存 map 读取
	if val, ok := p.memTokensMap.Load(agentID); ok {
		if num, ok2 := val.(uint64); ok2 {
			return num
		}
	}
	return 0
}

func (p *PricingService) subtractCachedTokens(ctx context.Context, agentID string, tokens uint64) {
	if p.redisClient != nil {
		key := fmt.Sprintf("pricing:accumulated:tokens:%s", agentID)
		// 减去 tokens。如果扣减后低于或等于 0 则删除该 Key，防止负数
		script := `
			local key = KEYS[1]
			local sub = tonumber(ARGV[1])
			local val = tonumber(redis.call('GET', key) or "0")
			local n = val - sub
			if n <= 0 then
				redis.call('DEL', key)
				return 0
			else
				redis.call('SET', key, tostring(n))
				return n
			end
		`
		_, _ = p.redisClient.Eval(ctx, script, []string{key}, strconv.FormatUint(tokens, 10)).Result()
		return
	}

	// 降级模式
	p.mu.Lock()
	defer p.mu.Unlock()
	if val, ok := p.memTokensMap.Load(agentID); ok {
		if num, ok2 := val.(uint64); ok2 {
			if num > tokens {
				p.memTokensMap.Store(agentID, num-tokens)
			} else {
				p.memTokensMap.Delete(agentID)
			}
		}
	}
}

// StartFlushWorker 启动后台延迟刷盘 Worker 协程
func (p *PricingService) StartFlushWorker(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Println("[Pricing Flush Worker] Stopping flush worker...")
				// 优雅退出前强制刷盘一次，防止内存遗漏
				flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				p.FlushUsage(flushCtx)
				cancel()
				return
			case <-ticker.C:
				p.FlushUsage(ctx)
			}
		}
	}()
}

// FlushUsage 将缓冲队列中的数据批量写入 SQLite，并减去对应的缓存 tokens 计数器
func (p *PricingService) FlushUsage(ctx context.Context) {
	var records []*model.UsageRecord

	if p.redisClient != nil {
		key := "pricing:usage:buffer"
		// 每次批量最多刷盘 500 条
		for i := 0; i < 500; i++ {
			val, err := p.redisClient.LPop(ctx, key).Result()
			if err != nil {
				break
			}
			var rec model.UsageRecord
			if err := json.Unmarshal([]byte(val), &rec); err == nil {
				records = append(records, &rec)
			}
		}
	} else {
		// 降级模式：从 channel 读取
		for i := 0; i < 500; i++ {
			select {
			case rec := <-p.memUsageBuffer:
				records = append(records, rec)
			default:
				break
			}
			if len(records) >= 500 {
				break
			}
		}
	}

	if len(records) == 0 {
		return
	}

	// 开启 SQLite 大事务批量插入
	if err := p.store.AppendUsageBatch(ctx, records); err != nil {
		log.Printf("[Pricing Flush Worker] Failed to save usage records batch to SQLite: %v", err)
		// 刷盘失败回滚，放回缓冲队列中以防丢数据
		if p.redisClient != nil {
			key := "pricing:usage:buffer"
			for _, rec := range records {
				val, _ := json.Marshal(rec)
				_ = p.redisClient.RPush(ctx, key, string(val)).Err()
			}
		} else {
			for _, rec := range records {
				select {
				case p.memUsageBuffer <- rec:
				default:
				}
			}
		}
		return
	}

	// 刷盘成功，按 AgentID 累计刷出的 tokens 额度并扣减缓存计数器
	agentTokensMap := make(map[string]uint64)
	for _, rec := range records {
		agentTokensMap[rec.AgentID] += rec.Tokens
	}

	for agentID, flushedTokens := range agentTokensMap {
		p.subtractCachedTokens(ctx, agentID, flushedTokens)
	}

	log.Printf("[Pricing Flush Worker] Flushed %d usage records to SQLite store successfully", len(records))
}

// ----- 配置解析（带短 TTL 缓存，降低 DB 读）-----

func (p *PricingService) resolveConfig(ctx context.Context, agentID string) *model.PriceConfig {
	p.mu.RLock()
	if c, ok := p.configCache[agentID]; ok {
		if exp, ok2 := p.cacheExpiry["cfg:" + agentID]; ok2 && time.Now().Before(exp) {
			p.mu.RUnlock()
			return c
		}
	}
	p.mu.RUnlock()

	cfg, err := p.store.GetPriceConfig(ctx, agentID)
	if err != nil || cfg == nil {
		cfg = p.defaultConfig(agentID)
	}

	p.mu.Lock()
	p.configCache[agentID] = cfg
	p.cacheExpiry["cfg:" + agentID] = time.Now().Add(p.cacheTTL)
	p.mu.Unlock()
	return cfg
}

func (p *PricingService) resolveSubscription(ctx context.Context, agentID string) *model.Subscription {
	p.mu.RLock()
	if s, ok := p.subCache[agentID]; ok {
		if exp, ok2 := p.cacheExpiry["sub:" + agentID]; ok2 && time.Now().Before(exp) {
			p.mu.RUnlock()
			return s
		}
	}
	p.mu.RUnlock()

	sub, err := p.store.GetSubscription(ctx, agentID)
	if err != nil || sub == nil {
		return nil
	}
	p.mu.Lock()
	p.subCache[agentID] = sub
	p.cacheExpiry["sub:" + agentID] = time.Now().Add(p.cacheTTL)
	p.mu.Unlock()
	return sub
}

func parseUint16(s string) (uint16, error) {
	v, err := strconv.ParseUint(s, 10, 16)
	if err != nil {
		return 0, err
	}
	return uint16(v), nil
}

func parseUint64(s string) (uint64, error) {
	return strconv.ParseUint(s, 10, 64)
}

func (p *PricingService) defaultConfig(agentID string) *model.PriceConfig {
	bps := defaultPlatformBps
	if v := os.Getenv("PLATFORM_BPS"); v != "" {
		if parsed, err := parseUint16(v); err == nil {
			bps = parsed
		}
	}
	svc := defaultServiceFee
	if v := os.Getenv("AGENT_SERVICE_FEE"); v != "" {
		if parsed, err := parseUint64(v); err == nil {
			svc = parsed
		}
	}
	return &model.PriceConfig{
		AgentID:       agentID,
		ModelCostPer1k: defaultModelCostPer1k,
		ServiceFee:     svc,
		PlatformBps:    bps,
	}
}
