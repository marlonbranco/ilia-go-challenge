package domain

import (
	"context"
	"errors"

	"github.com/shopspring/decimal"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrDuplicate         = errors.New("duplicate idempotency key")
	ErrInvalidAmount     = errors.New("amount must be greater than zero")
	ErrInvalidType       = errors.New("transaction type must be CREDIT or DEBIT")
	ErrInsufficientFunds = errors.New("insufficient funds: debit would produce negative balance")
)

type ListFilter struct {
	Type   *TransactionType
	Limit  int64
	Offset int64
}

type WalletRepository interface {
	GetOrCreateWallet(ctx context.Context, userID string) (Wallet, error)
	CreateTransaction(ctx context.Context, tx Transaction) (Transaction, error)
	GetBalance(ctx context.Context, userID string) (decimal.Decimal, error)
	ListTransactions(ctx context.Context, userID string, filter ListFilter) ([]Transaction, error)
	FindByIdempotencyKey(ctx context.Context, key string) (Transaction, error)
}
