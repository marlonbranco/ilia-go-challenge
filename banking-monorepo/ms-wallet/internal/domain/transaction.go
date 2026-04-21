package domain

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type TransactionType string

const (
	TransactionTypeCredit TransactionType = "CREDIT"
	TransactionTypeDebit  TransactionType = "DEBIT"
)

type Wallet struct {
	ID        string
	UserID    string
	Balance   decimal.Decimal
	UpdatedAt time.Time
}

type Transaction struct {
	ID             string
	UserID         string
	Type           TransactionType
	Amount         decimal.Decimal
	BalanceBefore  decimal.Decimal
	BalanceAfter   decimal.Decimal
	IdempotencyKey string
	Description    string
	CreatedAt      time.Time
}

func NewTransaction(
	userID string,
	txType TransactionType,
	amount decimal.Decimal,
	currentBalance decimal.Decimal,
	idempotencyKey string,
	description string,
) (Transaction, error) {
	if amount.LessThanOrEqual(decimal.Zero) {
		return Transaction{}, ErrInvalidAmount
	}

	if amount.Exponent() < -2 {
		return Transaction{}, ErrInvalidPrecision
	}

	if txType != TransactionTypeCredit && txType != TransactionTypeDebit {
		return Transaction{}, ErrInvalidType
	}

	var balanceAfter decimal.Decimal
	switch txType {
	case TransactionTypeCredit:
		balanceAfter = currentBalance.Add(amount)
	case TransactionTypeDebit:
		balanceAfter = currentBalance.Sub(amount)
		if balanceAfter.IsNegative() {
			return Transaction{}, ErrInsufficientFunds
		}
	}

	return Transaction{
		ID:             uuid.New().String(),
		UserID:         userID,
		Type:           txType,
		Amount:         amount,
		BalanceBefore:  currentBalance,
		BalanceAfter:   balanceAfter,
		IdempotencyKey: idempotencyKey,
		Description:    description,
		CreatedAt:      time.Now().UTC(),
	}, nil
}
