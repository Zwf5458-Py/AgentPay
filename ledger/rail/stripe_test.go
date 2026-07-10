package rail

import (
	"context"
	"testing"
)

func TestStripeRail_MockMode(t *testing.T) {
	rail := NewStripeRail("mock_test_key")

	if !rail.IsMock() {
		t.Error("Expected mock mode for mock_ prefixed key")
	}

	// Test Lock
	lockID, err := rail.Lock(context.Background(), "payer1", "agent1", 50000)
	if err != nil {
		t.Fatalf("Lock failed: %v", err)
	}
	expectedPrefix := "st_lock_agent1_50000"
	if lockID != expectedPrefix {
		t.Errorf("Expected lockID %s, got %s", expectedPrefix, lockID)
	}

	// Test Split (mock mode always succeeds)
	payouts := []Payout{
		{Target: "model_provider", Amount: 30000},
		{Target: "agent_owner", Amount: 20000},
	}
	err = rail.Split(context.Background(), lockID, payouts, "ext-data")
	if err != nil {
		t.Errorf("Mock Split should not error: %v", err)
	}

	// Test Verify (mock mode returns true for st_lock_ prefix)
	valid, err := rail.Verify(context.Background(), lockID)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if !valid {
		t.Error("Expected mock Verify to return true for st_lock_ prefix")
	}

	// Test Refund (mock mode always succeeds)
	err = rail.Refund(context.Background(), lockID, "test reason")
	if err != nil {
		t.Errorf("Mock Refund should not error: %v", err)
	}
}

func TestStripeRail_EmptyKey(t *testing.T) {
	rail := NewStripeRail("")

	if !rail.IsMock() {
		t.Error("Expected mock mode for empty key")
	}

	lockID, err := rail.Lock(context.Background(), "payer1", "agent1", 50000)
	if err != nil {
		t.Fatalf("Lock failed: %v", err)
	}
	if lockID == "" {
		t.Error("Expected non-empty lockID")
	}
}

func TestStripeRail_RealKeyMock(t *testing.T) {
	// 使用 test 模式真实密钥（sk_test_ 前缀）
	rail := NewStripeRail("sk_test_1234567890abcdef")

	if rail.IsMock() {
		t.Error("Expected real mode for sk_test_ key")
	}

	// 真实模式无法离线测试（需要网络），仅验证 Lock 不会同步 panic
	// 实际测试需要在集成测试环境进行
	_, err := rail.Lock(context.Background(), "payer1", "agent1", 50000)
	if err == nil {
		t.Skip("Stripe API call succeeded (unexpected in offline test)")
	}
	// 预期错误（网络不可达或无效密钥）
	t.Logf("Expected error in offline mode: %v", err)
}
