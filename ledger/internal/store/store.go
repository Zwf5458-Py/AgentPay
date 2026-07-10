package store

import (
	"context"
	"ledger/internal/model"
)

type LedgerStore interface {
	// CreateAccount 创建新账户
	CreateAccount(ctx context.Context, acc *model.Account) error

	// GetAccount 获取账户
	GetAccount(ctx context.Context, id string) (*model.Account, error)

	// UpdateBalance 更新账户余额
	UpdateBalance(ctx context.Context, id string, amount int64) error

	// CreateInvoice 创建计费单
	CreateInvoice(ctx context.Context, inv *model.Invoice) error

	// GetInvoice 获取计费单
	GetInvoice(ctx context.Context, id string) (*model.Invoice, error)

	// UpdateInvoiceStatus 更新计费单状态
	UpdateInvoiceStatus(ctx context.Context, id string, status model.InvoiceStatus) error

	// AddLedgerEntry 增加一笔记账记录，Nonce 幂等防重
	AddLedgerEntry(ctx context.Context, entry *model.LedgerEntry) error

	// GetLedgerEntryByNonce 通过幂等键查询记录
	GetLedgerEntryByNonce(ctx context.Context, nonce string) (*model.LedgerEntry, error)
}
