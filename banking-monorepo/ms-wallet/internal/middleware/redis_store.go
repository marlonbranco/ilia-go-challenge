package middleware

import (
	"context"
	"errors"
	"time"

	"ms-wallet/internal/domain"

	"github.com/redis/go-redis/v9"
)

type redisIdempotencyStore struct {
	client *redis.Client
}

func NewRedisIdempotencyStore(client *redis.Client) domain.IdempotencyStore {
	return &redisIdempotencyStore{client: client}
}

func (s *redisIdempotencyStore) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	return s.client.SetNX(ctx, key, value, ttl).Result()
}

func (s *redisIdempotencyStore) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return s.client.Set(ctx, key, value, ttl).Err()
}

func (s *redisIdempotencyStore) Get(ctx context.Context, key string) (string, error) {
	val, err := s.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrKeyNotFound
	}
	return val, err
}
