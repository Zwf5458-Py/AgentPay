package rail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type StripeRail struct {
	secretKey  string
	isMock     bool
	httpClient *http.Client
}

// NewStripeRail 构造器
// secretKey: Stripe 密钥。空或 mock_ 前缀表示 mock 模式
func NewStripeRail(secretKey string) *StripeRail {
	isMock := secretKey == "" || strings.HasPrefix(secretKey, "mock_")
	return &StripeRail{
		secretKey: secretKey,
		isMock:    isMock,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Lock 创建 Stripe Checkout 会话作为锁
// 返回 sessionID 作为 lockID
func (r *StripeRail) Lock(ctx context.Context, payer, agent string, amount uint64) (string, error) {
	if r.isMock {
		lockID := fmt.Sprintf("st_lock_%s_%d", agent, amount)
		log.Printf("[StripeRail] [Mock] Locked amount %d for agent %s. LockID: %s", amount, agent, lockID)
		return lockID, nil
	}

	// 真实模式：创建 Stripe Checkout Session
	// 金额转换为美分（Stripe 使用最小货币单位）
	cents := amount / 10000
	if cents == 0 && amount > 0 {
		cents = 1
	}

	data := url.Values{}
	data.Set("payment_method_types[0]", "card")
	data.Set("line_items[0][price_data][currency]", "usd")
	data.Set("line_items[0][price_data][product_data][name]", "Agent Payment")
	data.Set("line_items[0][price_data][unit_amount]", fmt.Sprintf("%d", cents))
	data.Set("line_items[0][quantity]", "1")
	data.Set("mode", "payment")
	data.Set("success_url", "https://agentpay.io/success")
	data.Set("cancel_url", "https://agentpay.io/cancel")
	data.Set("metadata[agent_id]", agent)
	data.Set("metadata[payer]", payer)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.stripe.com/v1/checkout/sessions", strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create stripe request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", r.secretKey))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute stripe request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var stripeErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&stripeErr); err == nil && stripeErr.Error.Message != "" {
			return "", fmt.Errorf("stripe api error (status %d): %s", resp.StatusCode, stripeErr.Error.Message)
		}
		return "", fmt.Errorf("stripe api error (status %d)", resp.StatusCode)
	}

	var sessionResp struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sessionResp); err != nil {
		return "", fmt.Errorf("failed to decode stripe response: %w", err)
	}

	log.Printf("[StripeRail] Created Stripe Checkout session %s for agent %s (amount: %d micro-units)", sessionResp.ID, agent, amount)
	return sessionResp.ID, nil
}

// Split 确认支付（Stripe 已付款，触发捕获或确认）
// extData 包含额外元数据（如 callID）
func (r *StripeRail) Split(ctx context.Context, lockID string, payouts []Payout, extData string) error {
	if r.isMock {
		log.Printf("[StripeRail] [Mock] Splitting payouts for LockID %s (extData length: %d):", lockID, len(extData))
		for _, p := range payouts {
			log.Printf(" - Payout to %s: %d", p.Target, p.Amount)
		}
		return nil
	}

	// 真实模式：确认支付状态（Stripe Checkout 已支付则执行分账）
	valid, err := r.Verify(ctx, lockID)
	if err != nil {
		return fmt.Errorf("failed to verify stripe session: %w", err)
	}
	if !valid {
		return fmt.Errorf("stripe session %s is not paid", lockID)
	}

	// Stripe 模式：直接记录分账日志（真实分账逻辑由 Stripe Connect/Transfers 处理）
	log.Printf("[StripeRail] Confirmed payment for session %s:", lockID)
	for _, p := range payouts {
		log.Printf(" - Payout to %s: %d micro-units", p.Target, p.Amount)
	}

	return nil
}

// Refund 原路退回（Stripe 模式：创建退款）
func (r *StripeRail) Refund(ctx context.Context, lockID string, reason string) error {
	if r.isMock {
		log.Printf("[StripeRail] [Mock] Refunding LockID %s, reason: %s", lockID, reason)
		return nil
	}

	// 真实模式：查询 session 获取 payment_intent，然后创建退款
	session, err := r.getSession(ctx, lockID)
	if err != nil {
		return fmt.Errorf("failed to get session for refund: %w", err)
	}

	if session.PaymentIntent == "" {
		return fmt.Errorf("no payment intent found for session %s", lockID)
	}

	data := url.Values{}
	data.Set("payment_intent", session.PaymentIntent)
	data.Set("reason", "requested_by_customer")

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.stripe.com/v1/refunds", strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create refund request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", r.secretKey))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute refund request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var stripeErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&stripeErr); err == nil && stripeErr.Error.Message != "" {
			return fmt.Errorf("stripe refund error (status %d): %s", resp.StatusCode, stripeErr.Error.Message)
		}
		return fmt.Errorf("stripe refund error (status %d)", resp.StatusCode)
	}

	log.Printf("[StripeRail] Refunded session %s, reason: %s", lockID, reason)
	return nil
}

// Verify 验证 Stripe 会话支付状态
func (r *StripeRail) Verify(ctx context.Context, lockID string) (bool, error) {
	if r.isMock {
		if strings.HasPrefix(lockID, "st_lock_") {
			return true, nil
		}
		return false, nil
	}

	session, err := r.getSession(ctx, lockID)
	if err != nil {
		return false, err
	}

	return session.PaymentStatus == "paid", nil
}

// getSession 获取 Stripe 会话详情
func (r *StripeRail) getSession(ctx context.Context, sessionID string) (*stripeSession, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("https://api.stripe.com/v1/checkout/sessions/%s", sessionID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create get session request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", r.secretKey))

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute get session request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var stripeErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&stripeErr); err == nil && stripeErr.Error.Message != "" {
			return nil, fmt.Errorf("stripe api error (status %d): %s", resp.StatusCode, stripeErr.Error.Message)
		}
		return nil, fmt.Errorf("stripe api error (status %d)", resp.StatusCode)
	}

	var session stripeSession
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, fmt.Errorf("failed to decode stripe session response: %w", err)
	}

	return &session, nil
}

type stripeSession struct {
	ID             string `json:"id"`
	PaymentStatus  string `json:"payment_status"`
	PaymentIntent  string `json:"payment_intent"`
	Status         string `json:"status"`
}

// IsMock 暴露 mock 状态供测试
func (r *StripeRail) IsMock() bool {
	return r.isMock
}

// 确保 bytes 被使用（避免未导入警告）
var _ = bytes.NewBuffer
var _ = io.Discard
