package repository_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"ms-wallet/internal/domain"
	walletRepo "ms-wallet/internal/repository"

	"github.com/shopspring/decimal"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	mongoImage = "mongo:7"

	errTestExpectedNoErrorMsg  = "expected no error, got %v"
	errTestExpectedErrMsg      = "expected %v, got %v"
)

func setupTestMongo(test *testing.T) (*walletRepo.MongoWalletRepository, func()) {
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        mongoImage,
		ExposedPorts: []string{"27017/tcp"},
		Cmd:          []string{"--replSet", "rs0", "--bind_ip_all"},
		WaitingFor: wait.ForLog("Waiting for connections").
			WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		test.Fatalf("failed to start mongo container: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		test.Fatalf("failed to get container host: %v", err)
	}

	port, err := container.MappedPort(ctx, "27017")
	if err != nil {
		test.Fatalf("failed to get container port: %v", err)
	}

	_, _, err = container.Exec(ctx, []string{
		"mongosh", "--eval",
		`rs.initiate({_id: "rs0", members: [{_id: 0, host: "localhost:27017"}]})`,
	})
	if err != nil {
		test.Fatalf("failed to initiate replica set: %v", err)
	}

	time.Sleep(3 * time.Second)

	uri := fmt.Sprintf("mongodb://%s:%s/?directConnection=true&replicaSet=rs0", host, port.Port())

	test.Setenv("MONGO_DB_NAME", "wallet_test")
	test.Setenv("MONGO_COLLECTION_PREFIX", "test_")

	repo, err := walletRepo.NewMongoWalletRepository(ctx, uri)
	if err != nil {
		test.Fatalf("failed to create repository: %v", err)
	}

	cleanup := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		repo.Close(shutdownCtx)
		if err := container.Terminate(shutdownCtx); err != nil {
			test.Logf("failed to terminate container: %v", err)
		}
	}

	return repo, cleanup
}

func TestGetOrCreateWallet(test *testing.T) {
	repo, cleanup := setupTestMongo(test)
	test.Cleanup(cleanup)

	ctx := context.Background()

	test.Run("creates wallet on first call", func(test *testing.T) {
		wallet, err := repo.GetOrCreateWallet(ctx, "user-new-1")
		if err != nil {
			test.Fatalf(errTestExpectedNoErrorMsg, err)
		}

		if wallet.UserID != "user-new-1" {
			test.Errorf("expected UserID user-new-1, got %s", wallet.UserID)
		}

		if !wallet.Balance.Equal(decimal.Zero) {
			test.Errorf("expected zero balance, got %s", wallet.Balance)
		}

		if wallet.ID.IsZero() {
			test.Error("expected non-zero ObjectID")
		}
	})

	test.Run("idempotent — second call returns same wallet", func(test *testing.T) {
		first, err := repo.GetOrCreateWallet(ctx, "user-idempotent-1")
		if err != nil {
			test.Fatalf(errTestExpectedNoErrorMsg, err)
		}

		second, err := repo.GetOrCreateWallet(ctx, "user-idempotent-1")
		if err != nil {
			test.Fatalf(errTestExpectedNoErrorMsg, err)
		}

		if first.ID != second.ID {
			test.Errorf("expected same wallet ID, got %v vs %v", first.ID, second.ID)
		}
	})
}

func TestCreateTransaction(test *testing.T) {
	repo, cleanup := setupTestMongo(test)
	test.Cleanup(cleanup)

	ctx := context.Background()

	test.Run("credit atomically updates wallet and inserts transaction", func(test *testing.T) {
		userID := "user-credit-1"
		_, err := repo.GetOrCreateWallet(ctx, userID)
		if err != nil {
			test.Fatalf("setup wallet: %v", err)
		}

		tx, err := domain.NewTransaction(userID, domain.TransactionTypeCredit, decimal.NewFromInt(500), decimal.Zero, "idem-credit-1", "deposit")
		if err != nil {
			test.Fatalf("domain: %v", err)
		}

		created, err := repo.CreateTransaction(ctx, tx)
		if err != nil {
			test.Fatalf(errTestExpectedNoErrorMsg, err)
		}

		if created.ID != tx.ID {
			test.Errorf("expected ID %v, got %v", tx.ID, created.ID)
		}

		balance, err := repo.GetBalance(ctx, userID)
		if err != nil {
			test.Fatalf("get balance: %v", err)
		}

		if !balance.Equal(decimal.NewFromInt(500)) {
			test.Errorf("expected balance 500, got %s", balance)
		}
	})

	test.Run("duplicate idempotency key returns ErrDuplicate", func(test *testing.T) {
		userID := "user-idem-dup-1"
		_, err := repo.GetOrCreateWallet(ctx, userID)
		if err != nil {
			test.Fatalf("setup wallet: %v", err)
		}

		tx, _ := domain.NewTransaction(userID, domain.TransactionTypeCredit, decimal.NewFromInt(100), decimal.Zero, "idem-dup-key", "first")
		_, err = repo.CreateTransaction(ctx, tx)
		if err != nil {
			test.Fatalf("first insert: %v", err)
		}

		tx2, _ := domain.NewTransaction(userID, domain.TransactionTypeCredit, decimal.NewFromInt(100), decimal.NewFromInt(100), "idem-dup-key", "duplicate")
		_, err = repo.CreateTransaction(ctx, tx2)
		if !errors.Is(err, domain.ErrDuplicate) {
			test.Errorf(errTestExpectedErrMsg, domain.ErrDuplicate, err)
		}
	})
}

func TestGetBalance(test *testing.T) {
	repo, cleanup := setupTestMongo(test)
	test.Cleanup(cleanup)

	ctx := context.Background()

	test.Run("returns zero for new wallet", func(test *testing.T) {
		userID := "user-balance-zero-1"
		_, err := repo.GetOrCreateWallet(ctx, userID)
		if err != nil {
			test.Fatalf(errTestExpectedNoErrorMsg, err)
		}

		balance, err := repo.GetBalance(ctx, userID)
		if err != nil {
			test.Fatalf(errTestExpectedNoErrorMsg, err)
		}

		if !balance.Equal(decimal.Zero) {
			test.Errorf("expected 0, got %s", balance)
		}
	})

	test.Run("returns ErrNotFound for nonexistent user", func(test *testing.T) {
		_, err := repo.GetBalance(ctx, "nonexistent-user-xyz")
		if !errors.Is(err, domain.ErrNotFound) {
			test.Errorf(errTestExpectedErrMsg, domain.ErrNotFound, err)
		}
	})
}

func TestListTransactions(test *testing.T) {
	repo, cleanup := setupTestMongo(test)
	test.Cleanup(cleanup)

	ctx := context.Background()
	userID := "user-list-1"

	_, err := repo.GetOrCreateWallet(ctx, userID)
	if err != nil {
		test.Fatalf("setup: %v", err)
	}

	balance := decimal.Zero
	for i := 0; i < 3; i++ {
		tx, _ := domain.NewTransaction(userID, domain.TransactionTypeCredit, decimal.NewFromInt(10), balance, fmt.Sprintf("list-key-%d", i), "")
		_, err := repo.CreateTransaction(ctx, tx)
		if err != nil {
			test.Fatalf("insert tx %d: %v", i, err)
		}
		balance = tx.BalanceAfter
	}

	debitTx, _ := domain.NewTransaction(userID, domain.TransactionTypeDebit, decimal.NewFromInt(5), balance, "list-key-debit", "")
	_, err = repo.CreateTransaction(ctx, debitTx)
	if err != nil {
		test.Fatalf("insert debit: %v", err)
	}

	test.Run("returns all transactions", func(test *testing.T) {
		txs, err := repo.ListTransactions(ctx, userID, domain.ListFilter{Limit: 10})
		if err != nil {
			test.Fatalf(errTestExpectedNoErrorMsg, err)
		}

		if len(txs) != 4 {
			test.Errorf("expected 4 transactions, got %d", len(txs))
		}
	})

	test.Run("filters by type CREDIT", func(test *testing.T) {
		txType := domain.TransactionTypeCredit
		txs, err := repo.ListTransactions(ctx, userID, domain.ListFilter{Type: &txType, Limit: 10})
		if err != nil {
			test.Fatalf(errTestExpectedNoErrorMsg, err)
		}

		if len(txs) != 3 {
			test.Errorf("expected 3 CREDIT transactions, got %d", len(txs))
		}
	})

	test.Run("pagination with limit and offset", func(test *testing.T) {
		txs, err := repo.ListTransactions(ctx, userID, domain.ListFilter{Limit: 2, Offset: 1})
		if err != nil {
			test.Fatalf(errTestExpectedNoErrorMsg, err)
		}

		if len(txs) != 2 {
			test.Errorf("expected 2 transactions with limit 2 offset 1, got %d", len(txs))
		}
	})
}

func TestFindByIdempotencyKey(test *testing.T) {
	repo, cleanup := setupTestMongo(test)
	test.Cleanup(cleanup)

	ctx := context.Background()
	userID := "user-idem-find-1"

	_, err := repo.GetOrCreateWallet(ctx, userID)
	if err != nil {
		test.Fatalf("setup: %v", err)
	}

	tx, _ := domain.NewTransaction(userID, domain.TransactionTypeCredit, decimal.NewFromInt(100), decimal.Zero, "idem-find-key", "")
	_, err = repo.CreateTransaction(ctx, tx)
	if err != nil {
		test.Fatalf("insert: %v", err)
	}

	test.Run("finds transaction by idempotency key", func(test *testing.T) {
		found, err := repo.FindByIdempotencyKey(ctx, "idem-find-key")
		if err != nil {
			test.Fatalf(errTestExpectedNoErrorMsg, err)
		}

		if found.ID != tx.ID {
			test.Errorf("expected ID %v, got %v", tx.ID, found.ID)
		}

		if found.IdempotencyKey != "idem-find-key" {
			test.Errorf("expected key idem-find-key, got %s", found.IdempotencyKey)
		}
	})

	test.Run("returns ErrNotFound for unknown key", func(test *testing.T) {
		_, err := repo.FindByIdempotencyKey(ctx, "nonexistent-key-xyz")
		if !errors.Is(err, domain.ErrNotFound) {
			test.Errorf(errTestExpectedErrMsg, domain.ErrNotFound, err)
		}
	})
}
