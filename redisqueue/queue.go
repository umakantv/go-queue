package redisqueue

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisQueue represents a Redis-backed queue implementation.
type RedisQueue struct {
	client    *redis.Client
	queueName string
}

// NewRedisQueue creates a new RedisQueue instance with the given Redis client and queue name.
func NewRedisQueue(client *redis.Client, queueName string) *RedisQueue {
	return &RedisQueue{
		client:    client,
		queueName: queueName,
	}
}

// Enqueue adds a value to the end of the queue.
func (q *RedisQueue) Enqueue(ctx context.Context, value string) error {
	return q.client.RPush(ctx, q.queueName, value).Err()
}

// Dequeue removes and returns a value from the front of the queue.
// It blocks until a value is available or the context is cancelled.
func (q *RedisQueue) Dequeue(ctx context.Context) (string, error) {
	result, err := q.client.LPop(ctx, q.queueName).Result()
	if err != nil {
		return "", err
	}
	return result, nil
}

// DequeueWithTimeout removes and returns a value from the front of the queue.
// It blocks until a value is available, the timeout is reached, or the context is cancelled.
func (q *RedisQueue) DequeueWithTimeout(ctx context.Context, timeout time.Duration) (string, error) {
	result, err := q.client.BRPop(ctx, timeout, q.queueName).Result()
	if err != nil {
		return "", err
	}
	if len(result) < 2 {
		return "", redis.Nil
	}
	return result[1], nil
}

// Size returns the number of elements in the queue.
func (q *RedisQueue) Size(ctx context.Context) (int64, error) {
	return q.client.LLen(ctx, q.queueName).Result()
}

// Clear removes all elements from the queue.
func (q *RedisQueue) Clear(ctx context.Context) error {
	return q.client.Del(ctx, q.queueName).Err()
}

// Peek returns elements from the queue without removing them.
// start and stop are 0-based indices. Use -1 for the last element.
func (q *RedisQueue) Peek(ctx context.Context, start, stop int64) ([]string, error) {
	return q.client.LRange(ctx, q.queueName, start, stop).Result()
}

// GetQueueName returns the name of the queue.
func (q *RedisQueue) GetQueueName() string {
	return q.queueName
}

// GetClient returns the Redis client used by the queue.
func (q *RedisQueue) GetClient() *redis.Client {
	return q.client
}
