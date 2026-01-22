package database

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis wraps a Redis client
type Redis struct {
	Client *redis.Client
}

// NewRedis creates a new Redis client
func NewRedis(addr string) (*Redis, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     "", // no password by default
		DB:           0,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
		MinIdleConns: 5,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return &Redis{Client: client}, nil
}

// Close closes the Redis connection
func (r *Redis) Close() error {
	return r.Client.Close()
}

// HealthCheck performs a health check on Redis
func (r *Redis) HealthCheck(ctx context.Context) error {
	return r.Client.Ping(ctx).Err()
}

// Set stores a value with optional expiration
func (r *Redis) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return r.Client.Set(ctx, key, value, expiration).Err()
}

// Get retrieves a value
func (r *Redis) Get(ctx context.Context, key string) (string, error) {
	return r.Client.Get(ctx, key).Result()
}

// Delete removes a key
func (r *Redis) Delete(ctx context.Context, keys ...string) error {
	return r.Client.Del(ctx, keys...).Err()
}

// Publish sends a message to a channel
func (r *Redis) Publish(ctx context.Context, channel string, message interface{}) error {
	return r.Client.Publish(ctx, channel, message).Err()
}

// Subscribe creates a subscription to channels
func (r *Redis) Subscribe(ctx context.Context, channels ...string) *redis.PubSub {
	return r.Client.Subscribe(ctx, channels...)
}
