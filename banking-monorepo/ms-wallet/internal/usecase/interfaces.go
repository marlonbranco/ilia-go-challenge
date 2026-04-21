package usecase

import (
	"context"

	"ms-wallet/internal/domain"

	"github.com/shopspring/decimal"
)

type TransactionService interface {
	CreateTransaction(ctx context.Context, req CreateTransactionRequest) (domain.Transaction, error)
	GetBalance(ctx context.Context, userID string) (decimal.Decimal, error)
	ListTransactions(ctx context.Context, userID string, filter domain.ListFilter) ([]domain.Transaction, error)
}
