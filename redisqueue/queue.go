package redisqueue

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisQueue represents a Redis-backed queue implementation.
type RedisQueue struct {
	client            *redis.Client
	queueName         string
	visibilityTimeout time.Duration
}

// NewRedisQueue creates a new RedisQueue instance with the given Redis client and queue name.
func NewRedisQueue(client *redis.Client, queueName string) *RedisQueue {
	return &RedisQueue{
		client:            client,
		queueName:         queueName,
		visibilityTimeout: 1 * time.Minute, // Default visibility timeout
	}
}

// SetVisibilityTimeout sets the visibility timeout for the queue.
func (q *RedisQueue) SetVisibilityTimeout(timeout time.Duration) {
	q.visibilityTimeout = timeout
}

// Enqueue adds a value to the queue with a priority score.
// Lower scores indicate higher priority.
func (q *RedisQueue) Enqueue(ctx context.Context, value string, priority int) error {
	return q.client.ZAdd(ctx, q.queueName, redis.Z{
		Score:  float64(priority),
		Member: value,
	}).Err()
}

// Dequeue removes and returns the highest priority value from the queue.
// It uses a processing set (Sorted Set) to handle visibility timeouts.
func (q *RedisQueue) Dequeue(ctx context.Context) (string, error) {
	// Atomically pop the member with the lowest score (highest priority)
	// and add it to the processing sorted set.
	// Using a Lua script to ensure atomicity.
	script := `
		local val = redis.call('ZRANGE', KEYS[1], 0, 0)[1]
		if val then
			redis.call('ZREM', KEYS[1], val)
			redis.call('ZADD', KEYS[2], ARGV[1], val)
		end
		return val
	`
	processingKey := q.GetProcessingKey()
	now := time.Now().Add(q.visibilityTimeout).UnixNano()

	val, err := q.client.Eval(ctx, script, []string{q.queueName, processingKey}, now).Result()
	if err != nil {
		if err == redis.Nil {
			return "", err
		}
		return "", fmt.Errorf("failed to dequeue with visibility timeout: %w", err)
	}
	if val == nil {
		return "", redis.Nil
	}
	return val.(string), nil
}

// Complete removes a job from the processing set.
func (q *RedisQueue) Complete(ctx context.Context, value string) error {
	return q.client.ZRem(ctx, q.GetProcessingKey(), value).Err()
}

// GetProcessingKey returns the key for the processing sorted set.
func (q *RedisQueue) GetProcessingKey() string {
	return fmt.Sprintf("%s:processing", q.queueName)
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
	return q.client.ZCard(ctx, q.queueName).Result()
}

// Clear removes all elements from the queue.
func (q *RedisQueue) Clear(ctx context.Context) error {
	return q.client.Del(ctx, q.queueName).Err()
}

// Peek returns elements from the queue without removing them.
// start and stop are 0-based indices. Use -1 for the last element.
func (q *RedisQueue) Peek(ctx context.Context, start, stop int64) ([]string, error) {
	return q.client.ZRange(ctx, q.queueName, start, stop).Result()
}

// GetQueueName returns the name of the queue.
func (q *RedisQueue) GetQueueName() string {
	return q.queueName
}

// GetClient returns the Redis client used by the queue.
func (q *RedisQueue) GetClient() *redis.Client {
	return q.client
}

// RemoveFromSet removes a member from a sorted set key.
func (q *RedisQueue) RemoveFromSet(ctx context.Context, key string, member string) error {
	return q.client.ZRem(ctx, key, member).Err()
}

// UpdateProcessingJob updates a job in the processing set with new JSON.
// It removes the old member (by rawJSON) and adds the new member with the same expiration time.
func (q *RedisQueue) UpdateProcessingJob(ctx context.Context, oldJSON, newJSON string, expiration time.Time) error {
	processingKey := q.GetProcessingKey()

	// Use a Lua script to atomically remove old and add new
	script := `
		local score = redis.call('ZSCORE', KEYS[1], ARGV[1])
		if score then
			redis.call('ZREM', KEYS[1], ARGV[1])
			redis.call('ZADD', KEYS[1], score, ARGV[2])
			return 1
		end
		return 0
	`
	result, err := q.client.Eval(ctx, script, []string{processingKey}, oldJSON, newJSON).Result()
	if err != nil {
		return fmt.Errorf("failed to update processing job: %w", err)
	}
	if result.(int64) == 0 {
		return fmt.Errorf("job not found in processing set")
	}
	return nil
}

// AddToSet adds a member to a sorted set key with a score.
func (q *RedisQueue) AddToSet(ctx context.Context, key string, score float64, member string) error {
	return q.client.ZAdd(ctx, key, redis.Z{Score: score, Member: member}).Err()
}

// RangeByScore returns members from a sorted set within a score range.
func (q *RedisQueue) RangeByScore(ctx context.Context, key string, min, max string, count int64) ([]string, error) {
	query := &redis.ZRangeBy{Min: min, Max: max}
	if count > 0 {
		query.Offset = 0
		query.Count = count
	}
	return q.client.ZRangeByScore(ctx, key, query).Result()
}
