package repository

import (
	"context"
	"time"

	"ms-users/internal/domain"

	"github.com/redis/go-redis/v9"
)

type redisRateLimitCounter struct {
	client *redis.Client
}

func NewRedisRateLimitCounter(client *redis.Client) domain.RateLimitCounter {
	return &redisRateLimitCounter{client: client}
}

func (r *redisRateLimitCounter) Increment(ctx context.Context, key string) (int64, error) {
	return r.client.Incr(ctx, key).Result()
}

func (r *redisRateLimitCounter) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return r.client.Expire(ctx, key, ttl).Err()
}
