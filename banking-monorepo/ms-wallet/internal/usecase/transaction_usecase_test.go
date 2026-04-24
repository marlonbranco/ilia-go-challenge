package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ms-wallet/internal/domain"
	"ms-wallet/internal/usecase"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const (
	errUnexpected  = "unexpected error: %v"
	errNilReceived = "expected error %v, got nil"
	errWrongErr    = "expected %v, got %v"
)

type fakeWalletRepo struct {
	mutex        sync.RWMutex
	wallets      map[string]domain.Wallet
	transactions map[string]domain.Transaction
	byIdemKey    map[string]domain.Transaction
}

func newFakeWalletRepo() *fakeWalletRepo {
	return &fakeWalletRepo{
		wallets:      make(map[string]domain.Wallet),
		transactions: make(map[string]domain.Transaction),
		byIdemKey:    make(map[string]domain.Transaction),
	}
}

func (repository *fakeWalletRepo) GetOrCreateWallet(_ context.Context, userID string) (domain.Wallet, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if wallet, ok := repository.wallets[userID]; ok {
		return wallet, nil
	}
	wallet := domain.Wallet{
		ID:      uuid.New().String(),
		UserID:  userID,
		Balance: decimal.Zero,
	}
	repository.wallets[userID] = wallet
	return wallet, nil
}

func (repository *fakeWalletRepo) CreateTransaction(_ context.Context, tx domain.Transaction) (domain.Transaction, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if _, exists := repository.byIdemKey[tx.IdempotencyKey]; exists {
		return domain.Transaction{}, fmt.Errorf("duplicate: %w", domain.ErrDuplicate)
	}
	wallet := repository.wallets[tx.UserID]
	if !wallet.Balance.Equal(tx.BalanceBefore) {
		return domain.Transaction{}, fmt.Errorf("balance conflict: %w", domain.ErrConflict)
	}
	wallet.Balance = tx.BalanceAfter
	repository.wallets[tx.UserID] = wallet
	repository.transactions[tx.ID] = tx
	repository.byIdemKey[tx.IdempotencyKey] = tx
	return tx, nil
}

func (repository *fakeWalletRepo) GetBalance(_ context.Context, userID string) (decimal.Decimal, error) {
	repository.mutex.RLock()
	defer repository.mutex.RUnlock()
	wallet, ok := repository.wallets[userID]
	if !ok {
		return decimal.Zero, fmt.Errorf("not found: %w", domain.ErrNotFound)
	}
	return wallet.Balance, nil
}

func (repository *fakeWalletRepo) ListTransactions(_ context.Context, userID string, filter domain.ListFilter) ([]domain.Transaction, error) {
	repository.mutex.RLock()
	defer repository.mutex.RUnlock()
	var results []domain.Transaction
	for _, tx := range repository.transactions {
		if tx.UserID != userID {
			continue
		}
		if filter.Type != nil && tx.Type != *filter.Type {
			continue
		}
		results = append(results, tx)
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := filter.Offset
	if offset >= int64(len(results)) {
		return []domain.Transaction{}, nil
	}
	results = results[offset:]
	if int64(len(results)) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (repository *fakeWalletRepo) FindByIdempotencyKey(_ context.Context, key string) (domain.Transaction, error) {
	repository.mutex.RLock()
	defer repository.mutex.RUnlock()
	tx, ok := repository.byIdemKey[key]
	if !ok {
		return domain.Transaction{}, fmt.Errorf("not found: %w", domain.ErrNotFound)
	}
	return tx, nil
}

func TestCreateTransaction(test *testing.T) {
	userDebit := "user-debit"
	userIdem := "user-idem"

	tests := []struct {
		name    string
		setup   func(repo *fakeWalletRepo)
		req     usecase.CreateTransactionRequest
		wantErr error
		check   func(test *testing.T, tx domain.Transaction)
	}{
		{
			name: "credit creates transaction and updates balance",
			req: usecase.CreateTransactionRequest{
				UserID:         "user-1",
				Type:           domain.TransactionTypeCredit,
				Amount:         decimal.NewFromInt(100),
				IdempotencyKey: "key-credit-1",
				Description:    "deposit",
			},
			check: func(test *testing.T, tx domain.Transaction) {
				if tx.ID == "" {
					test.Error("expected non-empty ID")
				}
				if !tx.BalanceAfter.Equal(decimal.NewFromInt(100)) {
					test.Errorf("expected BalanceAfter 100, got %s", tx.BalanceAfter)
				}
				if !tx.BalanceBefore.Equal(decimal.Zero) {
					test.Errorf("expected BalanceBefore 0, got %s", tx.BalanceBefore)
				}
			},
		},
		{
			name: "debit reduces balance correctly",
			setup: func(repository *fakeWalletRepo) {
				ctx := context.Background()
				repository.GetOrCreateWallet(ctx, userDebit)
				tx, _ := domain.NewTransaction(userDebit, domain.TransactionTypeCredit, decimal.NewFromInt(200), decimal.Zero, "seed-key", "")
				repository.CreateTransaction(ctx, tx)
			},
			req: usecase.CreateTransactionRequest{
				UserID:         userDebit,
				Type:           domain.TransactionTypeDebit,
				Amount:         decimal.NewFromInt(50),
				IdempotencyKey: "key-debit-1",
			},
			check: func(test *testing.T, tx domain.Transaction) {
				expected := decimal.NewFromInt(150)
				if !tx.BalanceAfter.Equal(expected) {
					test.Errorf("expected BalanceAfter 150, got %s", tx.BalanceAfter)
				}
			},
		},
		{
			name: "idempotent — same key returns existing transaction",
			setup: func(repository *fakeWalletRepo) {
				ctx := context.Background()
				repository.GetOrCreateWallet(ctx, userIdem)
				tx, _ := domain.NewTransaction(userIdem, domain.TransactionTypeCredit, decimal.NewFromInt(50), decimal.Zero, "idem-key", "")
				repository.CreateTransaction(ctx, tx)
			},
			req: usecase.CreateTransactionRequest{
				UserID:         userIdem,
				Type:           domain.TransactionTypeCredit,
				Amount:         decimal.NewFromInt(50),
				IdempotencyKey: "idem-key",
			},
			check: func(test *testing.T, tx domain.Transaction) {
				if !tx.Amount.Equal(decimal.NewFromInt(50)) {
					test.Errorf("expected returned existing tx with amount 50, got %s", tx.Amount)
				}
			},
		},
		{
			name: "insufficient funds returns ErrInsufficientFunds",
			req: usecase.CreateTransactionRequest{
				UserID:         "user-broke",
				Type:           domain.TransactionTypeDebit,
				Amount:         decimal.NewFromInt(500),
				IdempotencyKey: "key-broke-1",
			},
			wantErr: domain.ErrInsufficientFunds,
		},
		{
			name: "zero amount returns ErrInvalidAmount",
			req: usecase.CreateTransactionRequest{
				UserID:         "user-zero",
				Type:           domain.TransactionTypeCredit,
				Amount:         decimal.Zero,
				IdempotencyKey: "key-zero-1",
			},
			wantErr: domain.ErrInvalidAmount,
		},
		{
			name: "invalid type returns ErrInvalidType",
			req: usecase.CreateTransactionRequest{
				UserID:         "user-badtype",
				Type:           domain.TransactionType("INVALID"),
				Amount:         decimal.NewFromInt(10),
				IdempotencyKey: "key-badtype-1",
			},
			wantErr: domain.ErrInvalidType,
		},
	}

	for _, tc := range tests {
		test.Run(tc.name, func(test *testing.T) {
			repository := newFakeWalletRepo()
			if tc.setup != nil {
				tc.setup(repository)
			}
			uc := usecase.NewTransactionUseCase(repository)

			tx, err := uc.CreateTransaction(context.Background(), tc.req)

			if tc.wantErr != nil {
				if err == nil {
					test.Fatalf(errNilReceived, tc.wantErr)
				}
				if !errors.Is(err, tc.wantErr) {
					test.Fatalf(errWrongErr, tc.wantErr, err)
				}
				return
			}

			if err != nil {
				test.Fatalf(errUnexpected, err)
			}

			if tc.check != nil {
				tc.check(test, tx)
			}
		})
	}
}

func TestGetBalance(test *testing.T) {
	test.Run("auto-creates wallet with zero balance for new user", func(test *testing.T) {
		repository := newFakeWalletRepo()
		uc := usecase.NewTransactionUseCase(repository)

		balance, err := uc.GetBalance(context.Background(), "new-user")
		if err != nil {
			test.Fatalf(errUnexpected, err)
		}
		if !balance.Equal(decimal.Zero) {
			test.Errorf("expected 0, got %s", balance)
		}
	})

	test.Run("reflects balance after credit", func(test *testing.T) {
		repository := newFakeWalletRepo()
		uc := usecase.NewTransactionUseCase(repository)

		_, err := uc.CreateTransaction(context.Background(), usecase.CreateTransactionRequest{
			UserID:         "user-bal",
			Type:           domain.TransactionTypeCredit,
			Amount:         decimal.NewFromInt(300),
			IdempotencyKey: "bal-key-1",
		})
		if err != nil {
			test.Fatalf(errUnexpected, err)
		}

		balance, err := uc.GetBalance(context.Background(), "user-bal")
		if err != nil {
			test.Fatalf(errUnexpected, err)
		}
		if !balance.Equal(decimal.NewFromInt(300)) {
			test.Errorf("expected 300, got %s", balance)
		}
	})

	test.Run("reflects balance after credit then debit", func(test *testing.T) {
		repository := newFakeWalletRepo()
		uc := usecase.NewTransactionUseCase(repository)
		ctx := context.Background()
		userCd := "user-cd"

		uc.CreateTransaction(ctx, usecase.CreateTransactionRequest{
			UserID:         userCd,
			Type:           domain.TransactionTypeCredit,
			Amount:         decimal.NewFromInt(500),
			IdempotencyKey: "cd-key-1",
		})
		uc.CreateTransaction(ctx, usecase.CreateTransactionRequest{
			UserID:         userCd,
			Type:           domain.TransactionTypeDebit,
			Amount:         decimal.NewFromInt(200),
			IdempotencyKey: "cd-key-2",
		})

		balance, err := uc.GetBalance(ctx, userCd)
		if err != nil {
			test.Fatalf(errUnexpected, err)
		}
		if !balance.Equal(decimal.NewFromInt(300)) {
			test.Errorf("expected 300, got %s", balance)
		}
	})
}

func TestCredit(test *testing.T) {
	test.Run("credit convenience method creates CREDIT transaction", func(test *testing.T) {
		repo := newFakeWalletRepo()
		uc := usecase.NewTransactionUseCase(repo)

		tx, err := uc.Credit(context.Background(), "user-credit", decimal.NewFromInt(50), "credit-key-1", "salary")
		if err != nil {
			test.Fatalf(errUnexpected, err)
		}
		if tx.Type != domain.TransactionTypeCredit {
			test.Errorf("expected CREDIT, got %s", tx.Type)
		}
		if !tx.Amount.Equal(decimal.NewFromInt(50)) {
			test.Errorf("expected amount 50, got %s", tx.Amount)
		}
	})

	test.Run("credit rejects amount with more than 2 decimal places", func(test *testing.T) {
		repo := newFakeWalletRepo()
		uc := usecase.NewTransactionUseCase(repo)
		amount, _ := decimal.NewFromString("10.999")

		_, err := uc.Credit(context.Background(), "user-prec", amount, "prec-key-1", "")
		if !errors.Is(err, domain.ErrInvalidPrecision) {
			test.Errorf("expected ErrInvalidPrecision, got %v", err)
		}
	})
}

func TestDebit(test *testing.T) {
	test.Run("debit convenience method creates DEBIT transaction", func(test *testing.T) {
		repo := newFakeWalletRepo()
		uc := usecase.NewTransactionUseCase(repo)
		ctx := context.Background()

		uc.Credit(ctx, "user-d2", decimal.NewFromInt(200), "d2-seed", "")

		tx, err := uc.Debit(ctx, "user-d2", decimal.NewFromInt(75), "d2-debit-1", "rent")
		if err != nil {
			test.Fatalf(errUnexpected, err)
		}
		if tx.Type != domain.TransactionTypeDebit {
			test.Errorf("expected DEBIT, got %s", tx.Type)
		}
		expected := decimal.NewFromInt(125)
		if !tx.BalanceAfter.Equal(expected) {
			test.Errorf("expected BalanceAfter 125, got %s", tx.BalanceAfter)
		}
	})

	test.Run("debit returns ErrInsufficientFunds when balance is too low", func(test *testing.T) {
		repo := newFakeWalletRepo()
		uc := usecase.NewTransactionUseCase(repo)

		_, err := uc.Debit(context.Background(), "user-broke2", decimal.NewFromInt(100), "broke-key-1", "")
		if !errors.Is(err, domain.ErrInsufficientFunds) {
			test.Errorf("expected ErrInsufficientFunds, got %v", err)
		}
	})

	test.Run("debit rejects amount with more than 2 decimal places", func(test *testing.T) {
		repo := newFakeWalletRepo()
		uc := usecase.NewTransactionUseCase(repo)
		amount, _ := decimal.NewFromString("5.555")

		_, err := uc.Debit(context.Background(), "user-prec2", amount, "prec-key-2", "")
		if !errors.Is(err, domain.ErrInvalidPrecision) {
			test.Errorf("expected ErrInvalidPrecision, got %v", err)
		}
	})
}

func TestListTransactions(test *testing.T) {
	userList := "user-list"
	setup := func() (*fakeWalletRepo, *usecase.TransactionUseCase) {
		repository := newFakeWalletRepo()
		uc := usecase.NewTransactionUseCase(repository)
		ctx := context.Background()

		uc.CreateTransaction(ctx, usecase.CreateTransactionRequest{
			UserID: userList, Type: domain.TransactionTypeCredit,
			Amount: decimal.NewFromInt(100), IdempotencyKey: "list-k1",
		})
		uc.CreateTransaction(ctx, usecase.CreateTransactionRequest{
			UserID: userList, Type: domain.TransactionTypeCredit,
			Amount: decimal.NewFromInt(50), IdempotencyKey: "list-k2",
		})
		uc.CreateTransaction(ctx, usecase.CreateTransactionRequest{
			UserID: userList, Type: domain.TransactionTypeDebit,
			Amount: decimal.NewFromInt(30), IdempotencyKey: "list-k3",
		})
		return repository, uc
	}

	test.Run("returns all transactions for user", func(test *testing.T) {
		_, uc := setup()
		txs, err := uc.ListTransactions(context.Background(), userList, domain.ListFilter{Limit: 20})
		if err != nil {
			test.Fatalf(errUnexpected, err)
		}
		if len(txs) != 3 {
			test.Errorf("expected 3, got %d", len(txs))
		}
	})

	test.Run("filters by CREDIT type", func(test *testing.T) {
		_, uc := setup()
		txType := domain.TransactionTypeCredit
		txs, err := uc.ListTransactions(context.Background(), userList, domain.ListFilter{Type: &txType, Limit: 20})
		if err != nil {
			test.Fatalf(errUnexpected, err)
		}
		if len(txs) != 2 {
			test.Errorf("expected 2 CREDITs, got %d", len(txs))
		}
		for _, tx := range txs {
			if tx.Type != domain.TransactionTypeCredit {
				test.Errorf("expected CREDIT, got %s", tx.Type)
			}
		}
	})

	test.Run("filters by DEBIT type", func(test *testing.T) {
		_, uc := setup()
		txType := domain.TransactionTypeDebit
		txs, err := uc.ListTransactions(context.Background(), userList, domain.ListFilter{Type: &txType, Limit: 20})
		if err != nil {
			test.Fatalf(errUnexpected, err)
		}
		if len(txs) != 1 {
			test.Errorf("expected 1 DEBIT, got %d", len(txs))
		}
	})

	test.Run("returns empty for unknown user", func(test *testing.T) {
		_, uc := setup()
		txs, err := uc.ListTransactions(context.Background(), "ghost-user", domain.ListFilter{Limit: 20})
		if err != nil {
			test.Fatalf(errUnexpected, err)
		}
		if len(txs) != 0 {
			test.Errorf("expected 0, got %d", len(txs))
		}
	})

	test.Run("does not return other user's transactions", func(test *testing.T) {
		repo := newFakeWalletRepo()
		uc := usecase.NewTransactionUseCase(repo)
		ctx := context.Background()
		userA := "user-a"

		uc.CreateTransaction(ctx, usecase.CreateTransactionRequest{
			UserID: userA, Type: domain.TransactionTypeCredit,
			Amount: decimal.NewFromInt(100), IdempotencyKey: "sep-k1",
		})
		uc.CreateTransaction(ctx, usecase.CreateTransactionRequest{
			UserID: "user-B", Type: domain.TransactionTypeCredit,
			Amount: decimal.NewFromInt(100), IdempotencyKey: "sep-k2",
		})

		txs, err := uc.ListTransactions(ctx, userA, domain.ListFilter{Limit: 20})
		if err != nil {
			test.Fatalf(errUnexpected, err)
		}
		if len(txs) != 1 {
			test.Errorf("expected 1, got %d", len(txs))
		}
		if txs[0].UserID != userA {
			test.Errorf("got transaction belonging to wrong user: %s", txs[0].UserID)
		}
	})
}

type idempotencyRaceRepo struct {
	*fakeWalletRepo
	lookupCount  atomic.Int64
	precommitted domain.Transaction
}

func (r *idempotencyRaceRepo) FindByIdempotencyKey(_ context.Context, key string) (domain.Transaction, error) {
	if r.lookupCount.Add(1) == 1 {
		return domain.Transaction{}, fmt.Errorf("not found: %w", domain.ErrNotFound)
	}
	if key == r.precommitted.IdempotencyKey {
		return r.precommitted, nil
	}
	return domain.Transaction{}, fmt.Errorf("not found: %w", domain.ErrNotFound)
}

func (r *idempotencyRaceRepo) CreateTransaction(_ context.Context, _ domain.Transaction) (domain.Transaction, error) {
	return domain.Transaction{}, fmt.Errorf("duplicate: %w", domain.ErrDuplicate)
}

func TestIdempotencyRaceFallback(test *testing.T) {
	test.Run("ErrDuplicate falls back to FindByIdempotencyKey and returns committed transaction", func(test *testing.T) {
		committed, _ := domain.NewTransaction("user-race", domain.TransactionTypeCredit, decimal.NewFromInt(100), decimal.Zero, "race-key-1", "race deposit")

		repo := &idempotencyRaceRepo{
			fakeWalletRepo: newFakeWalletRepo(),
			precommitted:   committed,
		}
		uc := usecase.NewTransactionUseCase(repo)

		tx, err := uc.CreateTransaction(context.Background(), usecase.CreateTransactionRequest{
			UserID:         "user-race",
			Type:           domain.TransactionTypeCredit,
			Amount:         decimal.NewFromInt(100),
			IdempotencyKey: "race-key-1",
		})
		if err != nil {
			test.Fatalf("expected committed transaction via fallback, got error: %v", err)
		}
		if tx.ID != committed.ID {
			test.Errorf("expected committed tx ID %s, got %s", committed.ID, tx.ID)
		}
	})
}

type alwaysConflictRepo struct{ *fakeWalletRepo }

func (r *alwaysConflictRepo) CreateTransaction(_ context.Context, _ domain.Transaction) (domain.Transaction, error) {
	return domain.Transaction{}, fmt.Errorf("injected: %w", domain.ErrConflict)
}

type conflictOnceRepo struct {
	*fakeWalletRepo
	fired atomic.Bool
}

type conflictThenDrainRepo struct {
	*fakeWalletRepo
	fired   atomic.Bool
	drainTo decimal.Decimal
}

func (r *conflictThenDrainRepo) CreateTransaction(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) {
	if r.fired.CompareAndSwap(false, true) {
		r.fakeWalletRepo.mutex.Lock()
		if wallet, ok := r.fakeWalletRepo.wallets[tx.UserID]; ok {
			wallet.Balance = r.drainTo
			r.fakeWalletRepo.wallets[tx.UserID] = wallet
		}
		r.fakeWalletRepo.mutex.Unlock()
		return domain.Transaction{}, fmt.Errorf("injected: %w", domain.ErrConflict)
	}
	return r.fakeWalletRepo.CreateTransaction(ctx, tx)
}

func (r *conflictOnceRepo) CreateTransaction(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) {
	if r.fired.CompareAndSwap(false, true) {
		return domain.Transaction{}, fmt.Errorf("injected: %w", domain.ErrConflict)
	}
	return r.fakeWalletRepo.CreateTransaction(ctx, tx)
}

func TestConcurrentDebitInsufficientFundsOnRetry(test *testing.T) {
	test.Run("retry after conflict fails with ErrInsufficientFunds when concurrent debit drained balance", func(test *testing.T) {
		repo := newFakeWalletRepo()
		ctx := context.Background()

		repo.GetOrCreateWallet(ctx, "user-drain")
		seed, _ := domain.NewTransaction("user-drain", domain.TransactionTypeCredit, decimal.NewFromInt(100), decimal.Zero, "seed-drain", "")
		repo.CreateTransaction(ctx, seed)

		drainRepo := &conflictThenDrainRepo{
			fakeWalletRepo: repo,
			drainTo:        decimal.NewFromInt(20),
		}
		uc := usecase.NewTransactionUseCase(drainRepo)

		_, err := uc.Debit(ctx, "user-drain", decimal.NewFromInt(80), "drain-key-1", "")
		if !errors.Is(err, domain.ErrInsufficientFunds) {
			test.Fatalf("expected ErrInsufficientFunds after drained balance, got: %v", err)
		}
	})
}

func TestCreateTransactionRetryOnConflict(test *testing.T) {
	test.Run("retries and succeeds after one ErrConflict", func(test *testing.T) {
		repo := &conflictOnceRepo{fakeWalletRepo: newFakeWalletRepo()}
		uc := usecase.NewTransactionUseCase(repo)

		tx, err := uc.CreateTransaction(context.Background(), usecase.CreateTransactionRequest{
			UserID:         "user-retry",
			Type:           domain.TransactionTypeCredit,
			Amount:         decimal.NewFromInt(100),
			IdempotencyKey: "retry-key-1",
		})
		if err != nil {
			test.Fatalf(errUnexpected, err)
		}
		if !tx.BalanceAfter.Equal(decimal.NewFromInt(100)) {
			test.Errorf("expected BalanceAfter 100, got %s", tx.BalanceAfter)
		}
	})
}

func TestConcurrentCredits(test *testing.T) {
	test.Run("concurrent credits produce correct final balance", func(test *testing.T) {
		repo := newFakeWalletRepo()
		uc := usecase.NewTransactionUseCase(repo)
		ctx := context.Background()

		const goroutines = 10
		var wg sync.WaitGroup
		wg.Add(goroutines)
		for i := range goroutines {
			go func(i int) {
				defer wg.Done()
				_, err := uc.CreateTransaction(ctx, usecase.CreateTransactionRequest{
					UserID:         "user-concurrent",
					Type:           domain.TransactionTypeCredit,
					Amount:         decimal.NewFromInt(10),
					IdempotencyKey: fmt.Sprintf("concurrent-key-%d", i),
				})
				if err != nil {
					test.Errorf("goroutine %d: %v", i, err)
				}
			}(i)
		}
		wg.Wait()

		balance, err := uc.GetBalance(ctx, "user-concurrent")
		if err != nil {
			test.Fatalf(errUnexpected, err)
		}
		expected := decimal.NewFromInt(int64(goroutines) * 10)
		if !balance.Equal(expected) {
			test.Errorf("expected balance %s, got %s", expected, balance)
		}
	})
}

func TestHardCapExhaustedReturnsErrConflict(test *testing.T) {
	test.Run("returns ErrConflict after maxConflictRetries without hanging", func(test *testing.T) {
		repo := &alwaysConflictRepo{fakeWalletRepo: newFakeWalletRepo()}
		uc := usecase.NewTransactionUseCase(repo)

		_, err := uc.CreateTransaction(context.Background(), usecase.CreateTransactionRequest{
			UserID:         "user-cap",
			Type:           domain.TransactionTypeCredit,
			Amount:         decimal.NewFromInt(10),
			IdempotencyKey: uuid.New().String(),
		})
		if !errors.Is(err, domain.ErrConflict) {
			test.Fatalf("expected ErrConflict after cap exhaustion, got: %v", err)
		}
	})
}

func TestContextCancelledExitsRetryLoop(test *testing.T) {
	test.Run("already-cancelled context returns context.Canceled immediately", func(test *testing.T) {
		repo := &alwaysConflictRepo{fakeWalletRepo: newFakeWalletRepo()}
		uc := usecase.NewTransactionUseCase(repo)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := uc.CreateTransaction(ctx, usecase.CreateTransactionRequest{
			UserID:         "user-cancel",
			Type:           domain.TransactionTypeCredit,
			Amount:         decimal.NewFromInt(10),
			IdempotencyKey: uuid.New().String(),
		})
		if !errors.Is(err, context.Canceled) {
			test.Fatalf("expected context.Canceled, got: %v", err)
		}
	})

	test.Run("deadline exceeded during retries returns context.DeadlineExceeded", func(test *testing.T) {
		repo := &alwaysConflictRepo{fakeWalletRepo: newFakeWalletRepo()}
		uc := usecase.NewTransactionUseCase(repo)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()

		_, err := uc.CreateTransaction(ctx, usecase.CreateTransactionRequest{
			UserID:         "user-deadline",
			Type:           domain.TransactionTypeCredit,
			Amount:         decimal.NewFromInt(10),
			IdempotencyKey: uuid.New().String(),
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			test.Fatalf("expected context.DeadlineExceeded, got: %v", err)
		}
	})
}

func TestHighConcurrencyAllWritesSucceed(test *testing.T) {
	test.Run("50 concurrent credits all commit without any lost update", func(test *testing.T) {
		const goroutines = 50
		repo := newFakeWalletRepo()
		uc := usecase.NewTransactionUseCase(repo)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		barrier := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(goroutines)
		for i := range goroutines {
			go func(i int) {
				defer wg.Done()
				<-barrier
				_, err := uc.CreateTransaction(ctx, usecase.CreateTransactionRequest{
					UserID:         "user-hc",
					Type:           domain.TransactionTypeCredit,
					Amount:         decimal.NewFromInt(10),
					IdempotencyKey: fmt.Sprintf("hc-key-%d", i),
				})
				if err != nil {
					test.Errorf("goroutine %d: %v", i, err)
				}
			}(i)
		}
		close(barrier)
		wg.Wait()

		balance, err := uc.GetBalance(ctx, "user-hc")
		if err != nil {
			test.Fatalf(errUnexpected, err)
		}
		expected := decimal.NewFromInt(goroutines * 10)
		if !balance.Equal(expected) {
			test.Errorf("expected %s, got %s — lost update detected", expected, balance)
		}
	})
}

func TestConcurrentDecimalTransactions(test *testing.T) {
	test.Run("concurrent credits with decimal amounts produce exact balance", func(test *testing.T) {
		const goroutines = 40
		amount, _ := decimal.NewFromString("10.25")
		expected := amount.Mul(decimal.NewFromInt(goroutines))

		repo := newFakeWalletRepo()
		uc := usecase.NewTransactionUseCase(repo)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		barrier := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(goroutines)
		for i := range goroutines {
			go func(i int) {
				defer wg.Done()
				<-barrier
				_, err := uc.CreateTransaction(ctx, usecase.CreateTransactionRequest{
					UserID:         "user-dec-credit",
					Type:           domain.TransactionTypeCredit,
					Amount:         amount,
					IdempotencyKey: fmt.Sprintf("dec-credit-%d", i),
				})
				if err != nil {
					test.Errorf("goroutine %d: %v", i, err)
				}
			}(i)
		}
		close(barrier)
		wg.Wait()

		balance, err := uc.GetBalance(ctx, "user-dec-credit")
		if err != nil {
			test.Fatalf(errUnexpected, err)
		}
		if !balance.Equal(expected) {
			test.Errorf("expected %s, got %s — decimal precision lost under concurrency", expected, balance)
		}
	})

	test.Run("concurrent mixed credit/debit with decimals produces exact balance", func(test *testing.T) {
		const half = 20
		creditAmt, _ := decimal.NewFromString("5.50")
		debitAmt, _ := decimal.NewFromString("3.25")
		seed, _ := decimal.NewFromString("200.00")
		expected := seed.
			Add(creditAmt.Mul(decimal.NewFromInt(half))).
			Sub(debitAmt.Mul(decimal.NewFromInt(half)))

		repo := newFakeWalletRepo()
		uc := usecase.NewTransactionUseCase(repo)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Seed the wallet.
		_, err := uc.Credit(ctx, "user-dec-mixed", seed, "seed-mixed", "")
		if err != nil {
			test.Fatalf("seed: %v", err)
		}

		barrier := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(half * 2)
		for i := range half {
			go func(i int) {
				defer wg.Done()
				<-barrier
				_, err := uc.Credit(ctx, "user-dec-mixed", creditAmt, fmt.Sprintf("mixed-credit-%d", i), "")
				if err != nil {
					test.Errorf("credit goroutine %d: %v", i, err)
				}
			}(i)
			go func(i int) {
				defer wg.Done()
				<-barrier
				_, err := uc.Debit(ctx, "user-dec-mixed", debitAmt, fmt.Sprintf("mixed-debit-%d", i), "")
				if err != nil {
					test.Errorf("debit goroutine %d: %v", i, err)
				}
			}(i)
		}
		close(barrier)
		wg.Wait()

		balance, err := uc.GetBalance(ctx, "user-dec-mixed")
		if err != nil {
			test.Fatalf(errUnexpected, err)
		}
		if !balance.Equal(expected) {
			test.Errorf("expected %s, got %s — decimal precision lost under concurrency", expected, balance)
		}
	})

	test.Run("OCC filter handles decimal string normalisation correctly", func(test *testing.T) {
		// Verifies that amounts like "10.50" and "10.5" normalise to the same
		// decimal value and never cause a spurious conflict on retry.
		repo := newFakeWalletRepo()
		uc := usecase.NewTransactionUseCase(repo)
		ctx := context.Background()

		for i, raw := range []string{"10.50", "10.5", "10.50"} {
			amt, _ := decimal.NewFromString(raw)
			_, err := uc.Credit(ctx, "user-norm", amt, fmt.Sprintf("norm-key-%d", i), "")
			if err != nil {
				test.Fatalf("credit %q: %v", raw, err)
			}
		}

		balance, err := uc.GetBalance(ctx, "user-norm")
		if err != nil {
			test.Fatalf(errUnexpected, err)
		}
		expected, _ := decimal.NewFromString("31.50")
		if !balance.Equal(expected) {
			test.Errorf("expected %s, got %s", expected, balance)
		}
	})
}
