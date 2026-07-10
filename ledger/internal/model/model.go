package model

import (
	"time"
)

type AccountType string

const (
	AccountUser     AccountType = "user"
	AccountAgent    AccountType = "agent"
	AccountPlatform AccountType = "platform"
)

type Account struct {
	ID        string      `json:"id"`
	Type      AccountType `json:"type"`
	Balance   int64       `json:"balance"` // 微单位金额，正数
	CreatedAt time.Time   `json:"created_at"`
}

type EntryType string

const (
	Debit  EntryType = "debit"  // 账户扣减
	Credit EntryType = "credit" // 账户增加
)

type LedgerEntry struct {
	ID        string    `json:"id"`
	AccountID string    `json:"account_id"`
	Type      EntryType `json:"type"`
	Amount    uint64    `json:"amount"`
	RefType   string    `json:"ref_type"` // invoice / usage / refund
	RefID     string    `json:"ref_id"`
	Nonce     string    `json:"nonce"` // 幂等键
	CreatedAt time.Time `json:"created_at"`
}

type InvoiceStatus string

const (
	InvoicePending  InvoiceStatus = "pending"
	InvoiceSettled  InvoiceStatus = "settled"
	InvoiceRefunded InvoiceStatus = "refunded"
)

type Invoice struct {
	ID        string        `json:"id"`
	Payer     string        `json:"payer"`
	Agent     string        `json:"agent"`
	Amount    uint64        `json:"amount"`
	LockID    string        `json:"lock_id"` // 对应支付轨返回的锁存ID
	Status    InvoiceStatus `json:"status"`
	CreatedAt time.Time     `json:"created_at"`
}
