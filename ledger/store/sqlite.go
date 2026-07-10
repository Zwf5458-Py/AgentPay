package store

import (
	"context"
	"database/sql"
	"fmt"
	"ledger/model"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	// 拼接连接参数开启 WAL 模式提高并发性能
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if (err != nil) {
		return nil, err
	}

	store := &SQLiteStore{db: db}
	if err := store.bootstrap(); err != nil {
		db.Close()
		return nil, err
	}

	return store, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) bootstrap() error {
	schema := []string{
		`CREATE TABLE IF NOT EXISTS accounts (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			balance INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS invoices (
			id TEXT PRIMARY KEY,
			payer TEXT NOT NULL,
			agent TEXT NOT NULL,
			amount INTEGER NOT NULL,
			lock_id TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS ledger_entries (
			id TEXT PRIMARY KEY,
			account_id TEXT NOT NULL,
			type TEXT NOT NULL,
			amount INTEGER NOT NULL,
			ref_type TEXT NOT NULL,
			ref_id TEXT NOT NULL,
			nonce TEXT UNIQUE NOT NULL,
			created_at DATETIME NOT NULL
		);`,
	}

	for _, ddl := range schema {
		if _, err := s.db.Exec(ddl); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) CreateAccount(ctx context.Context, acc *model.Account) error {
	query := `INSERT INTO accounts (id, type, balance, created_at) VALUES (?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query, acc.ID, acc.Type, acc.Balance, acc.CreatedAt)
	return err
}

func (s *SQLiteStore) GetAccount(ctx context.Context, id string) (*model.Account, error) {
	query := `SELECT id, type, balance, created_at FROM accounts WHERE id = ?`
	row := s.db.QueryRowContext(ctx, query, id)

	var acc model.Account
	var typeStr string
	err := row.Scan(&acc.ID, &typeStr, &acc.Balance, &acc.CreatedAt)
	if (err != nil) {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	acc.Type = model.AccountType(typeStr)
	return &acc, nil
}

func (s *SQLiteStore) UpdateBalance(ctx context.Context, id string, amount int64) error {
	query := `UPDATE accounts SET balance = balance + ? WHERE id = ?`
	_, err := s.db.ExecContext(ctx, query, amount, id)
	return err
}

func (s *SQLiteStore) CreateInvoice(ctx context.Context, inv *model.Invoice) error {
	query := `INSERT INTO invoices (id, payer, agent, amount, lock_id, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query, inv.ID, inv.Payer, inv.Agent, inv.Amount, inv.LockID, inv.Status, inv.CreatedAt)
	return err
}

func (s *SQLiteStore) GetInvoice(ctx context.Context, id string) (*model.Invoice, error) {
	query := `SELECT id, payer, agent, amount, lock_id, status, created_at FROM invoices WHERE id = ?`
	row := s.db.QueryRowContext(ctx, query, id)

	var inv model.Invoice
	var statusStr string
	err := row.Scan(&inv.ID, &inv.Payer, &inv.Agent, &inv.Amount, &inv.LockID, &statusStr, &inv.CreatedAt)
	if (err != nil) {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	inv.Status = model.InvoiceStatus(statusStr)
	return &inv, nil
}

func (s *SQLiteStore) UpdateInvoiceStatus(ctx context.Context, id string, status model.InvoiceStatus) error {
	query := `UPDATE invoices SET status = ? WHERE id = ?`
	_, err := s.db.ExecContext(ctx, query, string(status), id)
	return err
}

func (s *SQLiteStore) AddLedgerEntry(ctx context.Context, entry *model.LedgerEntry) error {
	query := `INSERT INTO ledger_entries (id, account_id, type, amount, ref_type, ref_id, nonce, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query, entry.ID, entry.AccountID, string(entry.Type), entry.Amount, entry.RefType, entry.RefID, entry.Nonce, entry.CreatedAt)
	return err
}

func (s *SQLiteStore) GetLedgerEntryByNonce(ctx context.Context, nonce string) (*model.LedgerEntry, error) {
	query := `SELECT id, account_id, type, amount, ref_type, ref_id, nonce, created_at FROM ledger_entries WHERE nonce = ?`
	row := s.db.QueryRowContext(ctx, query, nonce)

	var entry model.LedgerEntry
	var typeStr string
	err := row.Scan(&entry.ID, &entry.AccountID, &typeStr, &entry.Amount, &entry.RefType, &entry.RefID, &entry.Nonce, &entry.CreatedAt)
	if (err != nil) {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	entry.Type = model.EntryType(typeStr)
	return &entry, nil
}
