package service

import (
	"context"
	"errors"
	"fmt"
	"ledger/model"
	"ledger/rail"
	"ledger/store"
	"time"

	"github.com/google/uuid"
)

type LedgerService struct {
	store store.LedgerStore
	rails map[string]rail.PaymentRail
}

func NewLedgerService(s store.LedgerStore, rails map[string]rail.PaymentRail) *LedgerService {
	return &LedgerService{
		store: s,
		rails: rails,
	}
}

// CreateInvoice 对应支付微锁存 (Lock)
func (s *LedgerService) CreateInvoice(ctx context.Context, payer, agent string, amount uint64, railType string) (*model.Invoice, error) {
	paymentRail, ok := s.rails[railType]
	if !ok {
		return nil, fmt.Errorf("unsupported payment rail type: %s", railType)
	}

	// 1. 调用支付轨锁定资金
	lockID, err := paymentRail.Lock(ctx, payer, agent, amount)
	if err != nil {
		return nil, fmt.Errorf("failed to lock funds on rail: %w", err)
	}

	// 2. 初始化对应账户以防外键异常
	if err := s.ensureAccount(ctx, payer, model.AccountUser); err != nil {
		return nil, err
	}
	if err := s.ensureAccount(ctx, agent, model.AccountAgent); err != nil {
		return nil, err
	}

	// 3. 写入 Invoice
	inv := &model.Invoice{
		ID:        uuid.New().String(),
		Payer:     payer,
		Agent:     agent,
		Amount:    amount,
		LockID:    lockID,
		Status:    model.InvoicePending,
		CreatedAt: time.Now(),
	}

	if err := s.store.CreateInvoice(ctx, inv); err != nil {
		return nil, fmt.Errorf("failed to save invoice: %w", err)
	}

	return inv, nil
}

// SettleInvoice 对应分账结算 (Split)
func (s *LedgerService) SettleInvoice(ctx context.Context, invoiceID string, actualCost uint64, payouts []rail.Payout, nonce string) error {
	// 1. 幂等校验 (通过查询借记条目判断该分账请求是否已做处理)
	existing, err := s.store.GetLedgerEntryByNonce(ctx, nonce+"_debit")
	if err == nil && existing != nil {
		// 已有相同幂等键的分账明细，直接重放成功
		return nil
	}

	// 2. 读取 Invoice
	inv, err := s.store.GetInvoice(ctx, invoiceID)
	if err != nil {
		return err
	}
	if inv == nil {
		return errors.New("invoice not found")
	}
	if inv.Status != model.InvoicePending {
		return fmt.Errorf("invoice is already in %s status", inv.Status)
	}

	// 3. 调用支付轨进行真实资金拆分结算
	// 这里默认将 "crypto" 或是 "stripe" 的支付轨取出来
	// 我们简单的从 lock_id 标识或直接通过默认 rail 调用
	var paymentRail rail.PaymentRail
	if len(inv.LockID) > 8 && inv.LockID[:8] == "st_lock_" {
		paymentRail = s.rails["stripe"]
	} else {
		paymentRail = s.rails["crypto"]
	}

	if paymentRail == nil {
		return errors.New("failed to resolve payment rail for settlement")
	}

	if err := paymentRail.Split(ctx, inv.LockID, payouts); err != nil {
		return fmt.Errorf("rail split settle failed: %w", err)
	}

	// 4. 双式记账变动 & 账户余额物理清算
	now := time.Now()
	
	// A. 扣减付款人账户 (Debit)
	debitEntry := &model.LedgerEntry{
		ID:        uuid.New().String(),
		AccountID: inv.Payer,
		Type:      model.Debit,
		Amount:    actualCost,
		RefType:   "usage",
		RefID:     inv.ID,
		Nonce:     nonce + "_debit",
		CreatedAt: now,
	}
	if err := s.store.AddLedgerEntry(ctx, debitEntry); err != nil {
		return err
	}
	if err := s.store.UpdateBalance(ctx, inv.Payer, -int64(actualCost)); err != nil {
		return err
	}

	// B. 增加收款人账户 (Credit)
	for i, p := range payouts {
		if err := s.ensureAccount(ctx, p.Target, model.AccountAgent); err != nil {
			return err
		}
		creditEntry := &model.LedgerEntry{
			ID:        uuid.New().String(),
			AccountID: p.Target,
			Type:      model.Credit,
			Amount:    p.Amount,
			RefType:   "usage",
			RefID:     inv.ID,
			Nonce:     fmt.Sprintf("%s_credit_%d", nonce, i),
			CreatedAt: now,
		}
		if err := s.store.AddLedgerEntry(ctx, creditEntry); err != nil {
			return err
		}
		if err := s.store.UpdateBalance(ctx, p.Target, int64(p.Amount)); err != nil {
			return err
		}
	}

	// C. 更新 Invoice 状态
	return s.store.UpdateInvoiceStatus(ctx, invoiceID, model.InvoiceSettled)
}

// ensureAccount 兜底初始化账户
func (s *LedgerService) ensureAccount(ctx context.Context, id string, accType model.AccountType) error {
	acc, err := s.store.GetAccount(ctx, id)
	if err != nil {
		return err
	}
	if acc == nil {
		newAcc := &model.Account{
			ID:        id,
			Type:      accType,
			Balance:   0,
			CreatedAt: time.Now(),
		}
		return s.store.CreateAccount(ctx, newAcc)
	}
	return nil
}
