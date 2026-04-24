package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"ms-wallet/internal/domain"

	"github.com/shopspring/decimal"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

const (
	walletsCollection      = "wallets"
	transactionsCollection = "transactions"

	errFailedToGetWalletMsg         = "failed to get or create wallet"
	errFailedToCreateTransactionMsg = "failed to create transaction"
	errFailedToGetBalanceMsg        = "failed to get balance"
	errFailedToListTransactionsMsg  = "failed to list transactions"
	errDuplicateIdempotencyKeyMsg   = "duplicate idempotency key: %w"
	errTransactionNotFoundMsg       = "transaction not found: %w"
)

type walletDocument struct {
	ID        bson.ObjectID `bson:"_id"`
	DomainID  string        `bson:"domain_id"`
	UserID    string        `bson:"user_id"`
	Balance   string        `bson:"balance"`
	UpdatedAt time.Time     `bson:"updated_at"`
}

type transactionDocument struct {
	ID             bson.ObjectID `bson:"_id"`
	DomainID       string        `bson:"domain_id"`
	UserID         string        `bson:"user_id"`
	Type           string        `bson:"type"`
	Amount         string        `bson:"amount"`
	BalanceBefore  string        `bson:"balance_before"`
	BalanceAfter   string        `bson:"balance_after"`
	IdempotencyKey string        `bson:"idempotency_key"`
	Description    string        `bson:"description"`
	CreatedAt      time.Time     `bson:"created_at"`
}

type MongoWalletRepository struct {
	client       *mongo.Client
	wallets      *mongo.Collection
	transactions *mongo.Collection
}

func NewMongoWalletRepository(ctx context.Context, uri, dbName, collectionPrefix string) (*MongoWalletRepository, error) {
	opts := options.Client().
		ApplyURI(uri).
		SetReadConcern(readconcern.Majority()).
		SetWriteConcern(writeconcern.Majority()).
		SetReadPreference(readpref.Primary())

	client, err := mongo.Connect(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to mongodb: %w", err)
	}

	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		return nil, fmt.Errorf("mongodb ping failed: %w", err)
	}

	if dbName == "" {
		dbName = "wallet"
	}

	db := client.Database(dbName)
	walletsCol := db.Collection(collectionPrefix + walletsCollection)
	transactionsCol := db.Collection(collectionPrefix + transactionsCollection)

	repo := &MongoWalletRepository{
		client:       client,
		wallets:      walletsCol,
		transactions: transactionsCol,
	}

	if err := repo.ensureIndexes(ctx); err != nil {
		return nil, fmt.Errorf("failed to ensure indexes: %w", err)
	}

	return repo, nil
}

func (repository *MongoWalletRepository) ensureIndexes(ctx context.Context) error {
	_, err := repository.transactions.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "idempotency_key", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return err
	}

	_, err = repository.transactions.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "user_id", Value: 1},
			{Key: "created_at", Value: -1},
		},
	})
	if err != nil {
		return err
	}

	_, err = repository.wallets.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "user_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	return err
}

func (repository *MongoWalletRepository) Close(ctx context.Context) error {
	return repository.client.Disconnect(ctx)
}

func (repository *MongoWalletRepository) GetOrCreateWallet(ctx context.Context, userID string) (domain.Wallet, error) {
	now := time.Now().UTC()
	domainID := newDomainID()
	filter := bson.D{{Key: "user_id", Value: userID}}
	update := bson.D{
		{Key: "$setOnInsert", Value: bson.D{
			{Key: "_id", Value: bson.NewObjectID()},
			{Key: "domain_id", Value: domainID},
			{Key: "user_id", Value: userID},
			{Key: "balance", Value: "0"},
			{Key: "updated_at", Value: now},
		}},
	}
	after := options.After
	opt := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(after)

	var doc walletDocument
	err := repository.wallets.FindOneAndUpdate(ctx, filter, update, opt).Decode(&doc)
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("%s: %w", errFailedToGetWalletMsg, err)
	}

	wallet, err := walletFromDoc(doc)
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("%s: %w", errFailedToGetWalletMsg, err)
	}
	return wallet, nil
}

func (repository *MongoWalletRepository) CreateTransaction(ctx context.Context, tx domain.Transaction) (domain.Transaction, error) {
	session, err := repository.client.StartSession()
	if err != nil {
		return domain.Transaction{}, fmt.Errorf("failed to start session: %w", err)
	}
	defer session.EndSession(ctx)

	_, err = session.WithTransaction(ctx, func(ctx context.Context) (any, error) {
		filter := bson.D{
			{Key: "user_id", Value: tx.UserID},
			{Key: "balance", Value: tx.BalanceBefore.String()},
		}
		update := bson.D{
			{Key: "$set", Value: bson.D{
				{Key: "balance", Value: tx.BalanceAfter.String()},
				{Key: "updated_at", Value: time.Now().UTC()},
			}},
		}
		result, updateErr := repository.wallets.UpdateOne(ctx, filter, update)
		if updateErr != nil {
			return nil, fmt.Errorf("wallet update: %w", updateErr)
		}
		if result.MatchedCount == 0 {
			return nil, fmt.Errorf("wallet balance conflict for user %s: %w", tx.UserID, domain.ErrConflict)
		}

		doc := transactionToDoc(tx)
		_, insertErr := repository.transactions.InsertOne(ctx, doc)
		if insertErr != nil {
			if mongo.IsDuplicateKeyError(insertErr) {
				return nil, fmt.Errorf(errDuplicateIdempotencyKeyMsg, domain.ErrDuplicate)
			}
			return nil, fmt.Errorf("transaction insert: %w", insertErr)
		}

		return nil, nil
	})
	if err != nil {
		if errors.Is(err, domain.ErrDuplicate) {
			return domain.Transaction{}, err
		}
		return domain.Transaction{}, fmt.Errorf("%s: %w", errFailedToCreateTransactionMsg, err)
	}

	return tx, nil
}

func (repository *MongoWalletRepository) GetBalance(ctx context.Context, userID string) (decimal.Decimal, error) {
	var doc walletDocument
	err := repository.wallets.FindOne(ctx, bson.D{{Key: "user_id", Value: userID}}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return decimal.Zero, fmt.Errorf(errTransactionNotFoundMsg, domain.ErrNotFound)
		}
		return decimal.Zero, fmt.Errorf("%s: %w", errFailedToGetBalanceMsg, err)
	}

	balance, err := decimal.NewFromString(doc.Balance)
	if err != nil {
		return decimal.Zero, fmt.Errorf("invalid stored balance: %w", err)
	}

	return balance, nil
}

func (repository *MongoWalletRepository) ListTransactions(ctx context.Context, userID string, filter domain.ListFilter) ([]domain.Transaction, error) {
	queryFilter := bson.D{{Key: "user_id", Value: userID}}
	if filter.Type != nil {
		queryFilter = append(queryFilter, bson.E{Key: "type", Value: string(*filter.Type)})
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}

	findOpts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetLimit(limit).
		SetSkip(filter.Offset)

	cursor, err := repository.transactions.Find(ctx, queryFilter, findOpts)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errFailedToListTransactionsMsg, err)
	}
	defer cursor.Close(ctx)

	var results []domain.Transaction
	for cursor.Next(ctx) {
		var doc transactionDocument
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("%s: %w", errFailedToListTransactionsMsg, err)
		}
		tx, err := transactionFromDoc(doc)
		if err != nil {
			return nil, err
		}
		results = append(results, tx)
	}

	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", errFailedToListTransactionsMsg, err)
	}

	return results, nil
}

func (repository *MongoWalletRepository) FindByIdempotencyKey(ctx context.Context, key string) (domain.Transaction, error) {
	var doc transactionDocument
	err := repository.transactions.FindOne(ctx, bson.D{{Key: "idempotency_key", Value: key}}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return domain.Transaction{}, fmt.Errorf(errTransactionNotFoundMsg, domain.ErrNotFound)
		}
		return domain.Transaction{}, err
	}

	return transactionFromDoc(doc)
}

func walletFromDoc(doc walletDocument) (domain.Wallet, error) {
	balance, err := decimal.NewFromString(doc.Balance)
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("wallet %s has invalid stored balance %q: %w", doc.DomainID, doc.Balance, err)
	}
	return domain.Wallet{
		ID:        doc.DomainID,
		UserID:    doc.UserID,
		Balance:   balance,
		UpdatedAt: doc.UpdatedAt,
	}, nil
}

func transactionToDoc(tx domain.Transaction) transactionDocument {
	return transactionDocument{
		ID:             bson.NewObjectID(),
		DomainID:       tx.ID,
		UserID:         tx.UserID,
		Type:           string(tx.Type),
		Amount:         tx.Amount.String(),
		BalanceBefore:  tx.BalanceBefore.String(),
		BalanceAfter:   tx.BalanceAfter.String(),
		IdempotencyKey: tx.IdempotencyKey,
		Description:    tx.Description,
		CreatedAt:      tx.CreatedAt,
	}
}

func transactionFromDoc(doc transactionDocument) (domain.Transaction, error) {
	amount, err := decimal.NewFromString(doc.Amount)
	if err != nil {
		return domain.Transaction{}, fmt.Errorf("invalid stored amount: %w", err)
	}
	before, err := decimal.NewFromString(doc.BalanceBefore)
	if err != nil {
		return domain.Transaction{}, fmt.Errorf("invalid stored balance_before: %w", err)
	}
	after, err := decimal.NewFromString(doc.BalanceAfter)
	if err != nil {
		return domain.Transaction{}, fmt.Errorf("invalid stored balance_after: %w", err)
	}

	return domain.Transaction{
		ID:             doc.DomainID,
		UserID:         doc.UserID,
		Type:           domain.TransactionType(doc.Type),
		Amount:         amount,
		BalanceBefore:  before,
		BalanceAfter:   after,
		IdempotencyKey: doc.IdempotencyKey,
		Description:    doc.Description,
		CreatedAt:      doc.CreatedAt,
	}, nil
}

func newDomainID() string {

	return bson.NewObjectID().Hex()
}
