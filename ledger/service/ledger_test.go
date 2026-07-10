package service

import (
	"context"
	"ledger/model"
	"ledger/rail"
	"ledger/store"
	"os"
	"testing"
)

func TestLedgerService(t *testing.T) {
	// 1. 创建临时的 SQLite 测试库文件
	dbFile := "./test_ledger.db"
	defer os.Remove(dbFile)

	sqliteStore, err := store.NewSQLiteStore(dbFile)
	if err != nil {
		t.Fatalf("failed to create SQLiteStore: %v", err)
	}
	defer sqliteStore.Close()

	// 2. 初始化 Mock StripeRail 支付轨
	stripeRail := rail.NewStripeRail(true)
	rails := map[string]rail.PaymentRail{
		"stripe": stripeRail,
	}

	ledgerSvc := NewLedgerService(sqliteStore, rails)
	ctx := context.Background()

	payer := "0xUserPayerAddress"
	agent := "0xAgentOwnerAddress"
	amount := uint64(1000000) // 1 USDC

	// 3. 测试 CreateInvoice (Lock)
	inv, err := ledgerSvc.CreateInvoice(ctx, payer, agent, amount, "stripe")
	if err != nil {
		t.Fatalf("failed to create invoice: %v", err)
	}

	if inv.Status != model.InvoicePending {
		t.Errorf("expected pending status, got %s", inv.Status)
	}

	// 验证账户余额是否已初始化为 0
	payerAcc, err := sqliteStore.GetAccount(ctx, payer)
	if err != nil || payerAcc == nil {
		t.Fatalf("failed to ensure payer account: %v", err)
	}
	if payerAcc.Balance != 0 {
		t.Errorf("expected balance 0, got %d", payerAcc.Balance)
	}

	// 4. 测试 SettleInvoice (Split)
	// 给 Payer 账户手动加钱模拟链上存托/法币锁存
	err = sqliteStore.UpdateBalance(ctx, payer, int64(amount))
	if err != nil {
		t.Fatalf("failed to simulate user balance load: %v", err)
	}

	payouts := []rail.Payout{
		{Target: agent, Amount: 900000},                // 0.9 USDC
		{Target: "0xPlatformAddress", Amount: 100000}, // 0.1 USDC
	}

	nonce := "unique_test_nonce_12345"
	err = ledgerSvc.SettleInvoice(ctx, inv.ID, amount, payouts, nonce)
	if err != nil {
		t.Fatalf("failed to settle invoice: %v", err)
	}

	// 验证账本记账后的物理余额
	payerAcc, _ = sqliteStore.GetAccount(ctx, payer)
	if payerAcc.Balance != 0 {
		t.Errorf("expected payer balance 0 after settle, got %d", payerAcc.Balance)
	}

	agentAcc, _ := sqliteStore.GetAccount(ctx, agent)
	if agentAcc.Balance != 900000 {
		t.Errorf("expected agent balance 900000, got %d", agentAcc.Balance)
	}

	platAcc, _ := sqliteStore.GetAccount(ctx, "0xPlatformAddress")
	if platAcc.Balance != 100000 {
		t.Errorf("expected platform balance 100000, got %d", platAcc.Balance)
	}

	// 验证计费单状态
	updatedInv, _ := sqliteStore.GetInvoice(ctx, inv.ID)
	if updatedInv.Status != model.InvoiceSettled {
		t.Errorf("expected invoice settled status, got %s", updatedInv.Status)
	}

	// 5. 验证幂等去重防重
	// 重新发送相同的 Settle 请求，它应当静默成功且不造成余额二次扣减！
	err = ledgerSvc.SettleInvoice(ctx, inv.ID, amount, payouts, nonce)
	if err != nil {
		t.Fatalf("expected silent success for idempotent split re-run, got error: %v", err)
	}

	// 再次验证余额并未改变，表明幂等正常运作
	payerAcc, _ = sqliteStore.GetAccount(ctx, payer)
	if payerAcc.Balance != 0 {
		t.Errorf("expected idempotent balance to remain 0, got %d", payerAcc.Balance)
	}
}
