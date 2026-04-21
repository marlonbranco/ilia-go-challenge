package domain_test

import (
	"testing"

	"ms-wallet/internal/domain"

	"github.com/shopspring/decimal"
)

var (
	testUserID         = "user-123"
	testIdempotencyKey = "idem-key-abc"
	testDescription    = "test payment"
	testBalance        = decimal.NewFromInt(100)
)

func TestNewTransaction(test *testing.T) {
	test.Run("valid credit creates transaction with correct BalanceAfter", func(test *testing.T) {
		amount := decimal.NewFromInt(50)
		tx, err := domain.NewTransaction(testUserID, domain.TransactionTypeCredit, amount, testBalance, testIdempotencyKey, testDescription)
		if err != nil {
			test.Fatalf("expected no error, got %v", err)
		}

		if tx.UserID != testUserID {
			test.Errorf("expected UserID %q, got %q", testUserID, tx.UserID)
		}

		if tx.Type != domain.TransactionTypeCredit {
			test.Errorf("expected type CREDIT, got %q", tx.Type)
		}

		if !tx.Amount.Equal(amount) {
			test.Errorf("expected amount %s, got %s", amount, tx.Amount)
		}

		expectedAfter := decimal.NewFromInt(150)
		if !tx.BalanceAfter.Equal(expectedAfter) {
			test.Errorf("expected BalanceAfter %s, got %s", expectedAfter, tx.BalanceAfter)
		}

		if !tx.BalanceBefore.Equal(testBalance) {
			test.Errorf("expected BalanceBefore %s, got %s", testBalance, tx.BalanceBefore)
		}

		if tx.ID == "" {
			test.Error("expected non-empty ID")
		}

		if tx.CreatedAt.IsZero() {
			test.Error("expected CreatedAt to be set")
		}
	})

	test.Run("valid debit reduces balance correctly", func(test *testing.T) {
		amount := decimal.NewFromInt(30)
		tx, err := domain.NewTransaction(testUserID, domain.TransactionTypeDebit, amount, testBalance, testIdempotencyKey, testDescription)
		if err != nil {
			test.Fatalf("expected no error, got %v", err)
		}

		expectedAfter := decimal.NewFromInt(70)
		if !tx.BalanceAfter.Equal(expectedAfter) {
			test.Errorf("expected BalanceAfter %s, got %s", expectedAfter, tx.BalanceAfter)
		}
	})

	test.Run("debit exactly equal to balance produces zero BalanceAfter", func(test *testing.T) {
		tx, err := domain.NewTransaction(testUserID, domain.TransactionTypeDebit, testBalance, testBalance, testIdempotencyKey, testDescription)
		if err != nil {
			test.Fatalf("expected no error for debit equal to balance, got %v", err)
		}

		if !tx.BalanceAfter.Equal(decimal.Zero) {
			test.Errorf("expected BalanceAfter 0, got %s", tx.BalanceAfter)
		}
	})

	test.Run("debit exceeding balance returns ErrInsufficientFunds", func(test *testing.T) {
		amount := decimal.NewFromInt(200)
		_, err := domain.NewTransaction(testUserID, domain.TransactionTypeDebit, amount, testBalance, testIdempotencyKey, testDescription)
		if err == nil {
			test.Fatal("expected ErrInsufficientFunds, got nil")
		}

		if err != domain.ErrInsufficientFunds {
			test.Errorf("expected ErrInsufficientFunds, got %v", err)
		}
	})

	test.Run("zero amount returns ErrInvalidAmount", func(test *testing.T) {
		_, err := domain.NewTransaction(testUserID, domain.TransactionTypeCredit, decimal.Zero, testBalance, testIdempotencyKey, testDescription)
		if err != domain.ErrInvalidAmount {
			test.Errorf("expected ErrInvalidAmount, got %v", err)
		}
	})

	test.Run("negative amount returns ErrInvalidAmount", func(test *testing.T) {
		_, err := domain.NewTransaction(testUserID, domain.TransactionTypeCredit, decimal.NewFromInt(-10), testBalance, testIdempotencyKey, testDescription)
		if err != domain.ErrInvalidAmount {
			test.Errorf("expected ErrInvalidAmount, got %v", err)
		}
	})

	test.Run("invalid type returns ErrInvalidType", func(test *testing.T) {
		_, err := domain.NewTransaction(testUserID, domain.TransactionType("INVALID"), decimal.NewFromInt(10), testBalance, testIdempotencyKey, testDescription)
		if err != domain.ErrInvalidType {
			test.Errorf("expected ErrInvalidType, got %v", err)
		}
	})

	test.Run("amount with 2 decimal places is accepted", func(test *testing.T) {
		amount, _ := decimal.NewFromString("9.99")
		_, err := domain.NewTransaction(testUserID, domain.TransactionTypeCredit, amount, testBalance, testIdempotencyKey, testDescription)
		if err != nil {
			test.Errorf("expected no error for 2dp amount, got %v", err)
		}
	})

	test.Run("amount with 3 decimal places returns ErrInvalidPrecision", func(test *testing.T) {
		amount, _ := decimal.NewFromString("9.999")
		_, err := domain.NewTransaction(testUserID, domain.TransactionTypeCredit, amount, testBalance, testIdempotencyKey, testDescription)
		if err != domain.ErrInvalidPrecision {
			test.Errorf("expected ErrInvalidPrecision, got %v", err)
		}
	})

	test.Run("amount with more than 2 decimal places returns ErrInvalidPrecision", func(test *testing.T) {
		amount, _ := decimal.NewFromString("1.123456")
		_, err := domain.NewTransaction(testUserID, domain.TransactionTypeCredit, amount, testBalance, testIdempotencyKey, testDescription)
		if err != domain.ErrInvalidPrecision {
			test.Errorf("expected ErrInvalidPrecision, got %v", err)
		}
	})
}
