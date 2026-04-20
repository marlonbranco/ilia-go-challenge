package usecase

import (
	"context"
	"errors"
	"fmt"

	"ms-wallet/internal/domain"

	"github.com/shopspring/decimal"
)

type TransactionUseCase struct {
	walletRepository domain.WalletRepository
}

func NewTransactionUseCase(walletRepository domain.WalletRepository) *TransactionUseCase {
	return &TransactionUseCase{walletRepository: walletRepository}
}

type CreateTransactionRequest struct {
	UserID         string
	Type           domain.TransactionType
	Amount         decimal.Decimal
	IdempotencyKey string
	Description    string
}

func (useCase *TransactionUseCase) CreateTransaction(ctx context.Context, request CreateTransactionRequest) (domain.Transaction, error) {
	existing, err := useCase.walletRepository.FindByIdempotencyKey(ctx, request.IdempotencyKey)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return domain.Transaction{}, fmt.Errorf("idempotency check: %w", err)
	}

	wallet, err := useCase.walletRepository.GetOrCreateWallet(ctx, request.UserID)
	if err != nil {
		return domain.Transaction{}, err
	}

	tx, err := domain.NewTransaction(request.UserID, request.Type, request.Amount, wallet.Balance, request.IdempotencyKey, request.Description)
	if err != nil {
		return domain.Transaction{}, err
	}

	return useCase.walletRepository.CreateTransaction(ctx, tx)
}

func (useCase *TransactionUseCase) GetBalance(ctx context.Context, userID string) (decimal.Decimal, error) {
	wallet, err := useCase.walletRepository.GetOrCreateWallet(ctx, userID)
	if err != nil {
		return decimal.Zero, err
	}
	return wallet.Balance, nil
}

func (useCase *TransactionUseCase) ListTransactions(ctx context.Context, userID string, filter domain.ListFilter) ([]domain.Transaction, error) {
	return useCase.walletRepository.ListTransactions(ctx, userID, filter)
}
