package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisTokenStore struct {
	client *redis.Client
}

func NewRedisTokenStore(client *redis.Client) *RedisTokenStore {
	return &RedisTokenStore{client: client}
}

func (store *RedisTokenStore) key(userID, tokenID string) string {
	return fmt.Sprintf("refresh:%s:%s", userID, tokenID)
}

func (store *RedisTokenStore) Store(ctx context.Context, userID, tokenID string, ttl time.Duration) error {
	return store.client.Set(ctx, store.key(userID, tokenID), "1", ttl).Err()
}

func (store *RedisTokenStore) Exists(ctx context.Context, userID, tokenID string) (bool, error) {
	n, err := store.client.Exists(ctx, store.key(userID, tokenID)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (store *RedisTokenStore) Delete(ctx context.Context, userID, tokenID string) error {
	return store.client.Del(ctx, store.key(userID, tokenID)).Err()
}
