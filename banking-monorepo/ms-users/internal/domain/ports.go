package domain

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound           = errors.New("user not found")
	ErrDuplicate          = errors.New("user already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidToken       = errors.New("invalid or expired token")
	ErrInvalidEmail       = errors.New("invalid email address")
	ErrInvalidPassword    = errors.New("password cannot be empty")
)

type UserRepository interface {
	Create(ctx context.Context, user User) (User, error)
	FindAll(ctx context.Context) ([]User, error)
	FindByEmail(ctx context.Context, email string) (User, error)
	FindByID(ctx context.Context, id uuid.UUID) (User, error)
	Update(ctx context.Context, user User) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type TokenStore interface {
	Store(ctx context.Context, userID, tokenID string, ttl time.Duration) error
	Exists(ctx context.Context, userID, tokenID string) (bool, error)
	Delete(ctx context.Context, userID, tokenID string) error
}

type WalletClient interface {
	GetBalance(ctx context.Context, userID string) (int64, error)
	ValidateUser(ctx context.Context, userID string) (bool, error)
	Close() error
}

type TokenService interface {
	GenerateTokenPair(userID, email string) (accessToken, refreshTokenID string, refreshTTL time.Duration, err error)
	ParseUnvalidated(token string) (userID, email string, err error)
}

type RateLimitCounter interface {
	Increment(ctx context.Context, key string) (int64, error)
	Expire(ctx context.Context, key string, ttl time.Duration) error
}
