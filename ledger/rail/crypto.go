package rail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type CryptoRail struct {
	aaBridgeURL    string
	internalSecret string
	httpClient     *http.Client
}

type CryptoSettleMetadata struct {
	Proof         string `json:"proof"`
	Signature     string `json:"signature"`
	Nonce         uint64 `json:"nonce"`
	Expiration    uint64 `json:"expiration"`
	HoldAmount    uint64 `json:"hold_amount"`
	ModelCost     uint64 `json:"model_cost"`
	ServiceFee    uint64 `json:"service_fee"`
	ModelProvider string `json:"model_provider"`
	Treasury      string `json:"treasury"`
	PlatformBps   uint16 `json:"platform_bps"`
	AgentID       int64  `json:"agent_id"`
	AgentOwner    string `json:"agent_owner"`
	EscrowAddress string `json:"escrow_address"`
}

func NewCryptoRail(aaBridgeURL string, internalSecret string) *CryptoRail {
	return &CryptoRail{
		aaBridgeURL:    aaBridgeURL,
		internalSecret: internalSecret,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (r *CryptoRail) Lock(ctx context.Context, payer, agent string, amount uint64) (string, error) {
	return payer, nil
}

// Split 发起向 aa-bridge 结算清算通道
func (r *CryptoRail) Split(ctx context.Context, lockID string, payouts []Payout, extData string) error {
	var meta CryptoSettleMetadata
	if err := json.Unmarshal([]byte(extData), &meta); err != nil {
		return fmt.Errorf("failed to unmarshal crypto settle metadata: %w", err)
	}

	// 计算总清算金额 accumulatedAmount
	var accumulatedAmount uint64 = 0
	for _, p := range payouts {
		accumulatedAmount += p.Amount
	}

	targetURL := strings.ReplaceAll(r.aaBridgeURL, "/aa/settle", "/aa/split-settle")
	bodyMap := map[string]interface{}{
		"channelId":         lockID,
		"accumulatedAmount": strconv.FormatUint(accumulatedAmount, 10),
		"modelCost":         strconv.FormatUint(meta.ModelCost, 10),
		"serviceFee":        strconv.FormatUint(meta.ServiceFee, 10),
		"modelProvider":     meta.ModelProvider,
		"treasury":          meta.Treasury,
		"platformBps":       meta.PlatformBps,
		"holdAmount":        strconv.FormatUint(meta.HoldAmount, 10),
		"nonce":             strconv.FormatUint(meta.Nonce, 10),
		"expiration":        strconv.FormatUint(meta.Expiration, 10),
		"signature":         meta.Signature,
		"agentId":           meta.AgentID,
		"proof":             meta.Proof,
		"agentOwner":        meta.AgentOwner,
		"escrowAddress":     meta.EscrowAddress,
	}

	bodyBytes, err := json.Marshal(bodyMap)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if r.internalSecret != "" {
		req.Header.Set("X-Internal-Secret", r.internalSecret)
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to dispatch split transaction: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("bridge returned http error %s: %s", resp.Status, string(body))
	}

	return nil
}

func (r *CryptoRail) Refund(ctx context.Context, lockID string, reason string) error {
	return nil
}

func (r *CryptoRail) Verify(ctx context.Context, lockID string) (bool, error) {
	return true, nil
}
