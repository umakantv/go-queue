package redisqueue

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// WorkerConfig holds configuration for a worker.
type WorkerConfig struct {
	Concurrency   int           // Number of concurrent goroutines per job type
	PollInterval  time.Duration // Interval between polling attempts
	RetryOnErr    bool          // Whether to retry failed jobs
	RetryDelay    time.Duration // Delay before retrying a failed job
	MaxRetries    int           // Maximum number of retries for failed jobs
}

// DefaultWorkerConfig returns a WorkerConfig with sensible defaults.
func DefaultWorkerConfig() WorkerConfig {
	return WorkerConfig{
		Concurrency:  1,
		PollInterval: 100 * time.Millisecond,
		RetryOnErr:   false,
		RetryDelay:   1 * time.Second,
		MaxRetries:   3,
	}
}

// Worker processes jobs from registered queues with configurable concurrency.
type Worker struct {
	registry *Registry
	config   WorkerConfig
	wg       sync.WaitGroup
	stopCh   chan struct{}
}

// NewWorker creates a new Worker for the given registry with default config.
func NewWorker(registry *Registry) *Worker {
	return NewWorkerWithConfig(registry, DefaultWorkerConfig())
}

// NewWorkerWithConfig creates a new Worker with custom configuration.
func NewWorkerWithConfig(registry *Registry, config WorkerConfig) *Worker {
	if config.Concurrency < 1 {
		config.Concurrency = 1
	}
	if config.PollInterval < 1*time.Millisecond {
		config.PollInterval = 100 * time.Millisecond
	}
	return &Worker{
		registry: registry,
		config:   config,
		stopCh:   make(chan struct{}),
	}
}

// Start begins processing jobs for the specified job types.
// If jobTypes is empty, all registered job types are processed.
// Each job type gets 'Concurrency' number of goroutines polling its queue.
func (w *Worker) Start(ctx context.Context, jobTypes ...string) {
	types := jobTypes
	if len(types) == 0 {
		types = w.registry.RegisteredTypes()
	}

	for _, jt := range types {
		for i := 0; i < w.config.Concurrency; i++ {
			w.wg.Add(1)
			workerID := i + 1
			go w.run(ctx, jt, workerID)
		}
		log.Printf("Started %d concurrent worker(s) for job type %q", w.config.Concurrency, jt)
	}
}

// run continuously processes jobs from a specific queue.
func (w *Worker) run(ctx context.Context, jobType string, workerID int) {
	defer w.wg.Done()

	queue, ok := w.registry.GetQueue(jobType)
	if !ok {
		log.Printf("[Worker-%d/%s] queue not found", workerID, jobType)
		return
	}

	handler := queue.GetHandler()

	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			log.Printf("[Worker-%d/%s] stopping", workerID, jobType)
			return
		case <-ctx.Done():
			log.Printf("[Worker-%d/%s] context cancelled", workerID, jobType)
			return
		case <-ticker.C:
			// Try to dequeue and process a job
			job, err := queue.DequeueJob(ctx)
			if err != nil {
				if err != redis.Nil {
					log.Printf("[Worker-%d/%s] error dequeuing: %v", workerID, jobType, err)
				}
				continue
			}

			// Process the job with optional retry
			w.processWithRetry(ctx, job, handler, workerID)
		}
	}
}

// processWithRetry processes a job with optional retry logic.
func (w *Worker) processWithRetry(ctx context.Context, job *Job, handler Handler, workerID int) {
	var lastErr error
	attempts := 0
	maxAttempts := 1
	if w.config.RetryOnErr {
		maxAttempts = w.config.MaxRetries + 1
	}

	for attempts < maxAttempts {
		attempts++
		err := handler.Handle(ctx, job)
		if err == nil {
			log.Printf("[Worker-%d] successfully processed job %s (type: %s)", workerID, job.ID, job.Type)
			return
		}
		lastErr = err

		if attempts < maxAttempts {
			log.Printf("[Worker-%d] job %s failed (attempt %d/%d): %v, retrying...",
				workerID, job.ID, attempts, maxAttempts, err)
			select {
			case <-time.After(w.config.RetryDelay):
			case <-ctx.Done():
				log.Printf("[Worker-%d] retry cancelled for job %s", workerID, job.ID)
				return
			}
		}
	}

	log.Printf("[Worker-%d] job %s (type: %s) failed after %d attempts: %v",
		workerID, job.ID, job.Type, attempts, lastErr)
}

// Stop signals the worker to stop processing jobs.
func (w *Worker) Stop() {
	close(w.stopCh)
	w.wg.Wait()
}

// ProcessAll processes all available jobs for the given job types once.
// This is useful for testing or one-off processing.
func (w *Worker) ProcessAll(ctx context.Context, jobTypes ...string) error {
	types := jobTypes
	if len(types) == 0 {
		types = w.registry.RegisteredTypes()
	}

	for _, jt := range types {
		queue, ok := w.registry.GetQueue(jt)
		if !ok {
			return fmt.Errorf("job type %q is not registered", jt)
		}

		for {
			job, err := queue.DequeueJob(ctx)
			if err != nil {
				if err == redis.Nil {
					break // Queue is empty
				}
				return fmt.Errorf("error dequeuing job type %q: %w", jt, err)
			}

			if err := queue.GetHandler().Handle(ctx, job); err != nil {
				log.Printf("Error processing job %s (type: %s): %v", job.ID, job.Type, err)
			}
		}
	}

	return nil
}

// GetConfig returns the worker configuration.
func (w *Worker) GetConfig() WorkerConfig {
	return w.config
}
