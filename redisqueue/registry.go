package redisqueue

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)


// Registry manages job type registrations and their associated queues.
type Registry struct {
	mu       sync.RWMutex
	client   *redis.Client
	queues   map[string]*JobQueue // job type -> job queue
	handlers map[string]Handler   // job type -> handler
}

// NewRegistry creates a new job registry with the given Redis client.
func NewRegistry(client *redis.Client) *Registry {
	return &Registry{
		client:   client,
		queues:   make(map[string]*JobQueue),
		handlers: make(map[string]Handler),
	}
}

// Register registers a new job type with its handler and queue name.
// The queueName is the Redis list key that will store jobs of this type.
func (r *Registry) Register(jobType string, queueName string, handler Handler) error {
	return r.RegisterWithVisibility(jobType, queueName, handler, 1*time.Minute)
}

// RegisterWithVisibility registers a new job type with a specific visibility timeout.
func (r *Registry) RegisterWithVisibility(jobType string, queueName string, handler Handler, visibilityTimeout time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.handlers[jobType]; exists {
		return fmt.Errorf("job type %q is already registered", jobType)
	}

	redisQueue := NewRedisQueue(r.client, queueName)
	redisQueue.SetVisibilityTimeout(visibilityTimeout)
	jobQueue := NewJobQueue(redisQueue, jobType, handler)

	r.queues[jobType] = jobQueue
	r.handlers[jobType] = handler

	return nil
}


// RegisterFunc is a convenience method to register a handler function.
func (r *Registry) RegisterFunc(jobType string, queueName string, handler HandlerFunc) error {
	return r.Register(jobType, queueName, handler)
}

// Enqueue adds a new job to the queue for its registered job type with optional max retries.
func (r *Registry) Enqueue(ctx context.Context, jobType string, payload any, maxRetries int) (*Job, error) {
	r.mu.RLock()
	queue, exists := r.queues[jobType]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("job type %q is not registered", jobType)
	}

	job, err := NewJob(jobType, payload, maxRetries)
	if err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}

	if err := queue.EnqueueJob(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to enqueue job: %w", err)
	}

	return job, nil
}


// GetQueue returns the JobQueue for the given job type.
func (r *Registry) GetQueue(jobType string) (*JobQueue, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	queue, ok := r.queues[jobType]
	return queue, ok
}

// GetHandler returns the Handler for the given job type.
func (r *Registry) GetHandler(jobType string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	handler, ok := r.handlers[jobType]
	return handler, ok
}

// ProcessOne processes a single job from the specified job type's queue.
func (r *Registry) ProcessOne(ctx context.Context, jobType string) error {
	r.mu.RLock()
	queue, exists := r.queues[jobType]
	r.mu.RUnlock()

	if !exists {
		return fmt.Errorf("job type %q is not registered", jobType)
	}

	return queue.Process(ctx)
}

// RegisteredTypes returns a list of all registered job types.
func (r *Registry) RegisteredTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.handlers))
	for jobType := range r.handlers {
		types = append(types, jobType)
	}
	return types
}

// Unregister removes a job type registration.
func (r *Registry) Unregister(jobType string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.queues, jobType)
	delete(r.handlers, jobType)
}
