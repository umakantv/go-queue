package redisqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// WorkerConfig holds configuration for a worker.
type WorkerConfig struct {
	Concurrency    int           // Number of concurrent goroutines per job type
	PollInterval   time.Duration // Interval between polling attempts
	RetryOnErr     bool          // Whether to retry failed jobs
	RetryDelay     time.Duration // Delay before retrying a failed job
	MaxRetries     int           // Maximum number of retries for failed jobs
	DelayBatchSize int64         // Max jobs to promote from delayed queue per tick
}

// DefaultWorkerConfig returns a WorkerConfig with sensible defaults.
func DefaultWorkerConfig() WorkerConfig {
	return WorkerConfig{
		Concurrency:    1,
		PollInterval:   100 * time.Millisecond,
		RetryOnErr:     false,
		RetryDelay:     1 * time.Second,
		MaxRetries:     3,
		DelayBatchSize: 50,
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
		w.wg.Add(1)
		go w.promoteDelayed(ctx, jt)
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
			w.processWithRetry(ctx, job, handler, queue, workerID)
		}
	}
}

// processWithRetry processes a job with optional retry logic.
func (w *Worker) processWithRetry(ctx context.Context, job *Job, handler Handler, queue *JobQueue, workerID int) {
	maxAttempts := w.config.MaxRetries
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	attempt := job.RetryCount + 1
	err := handler.Handle(ctx, job)
	if err == nil {
		log.Printf("[Worker-%d] successfully processed job %s (type: %s)", workerID, job.ID, job.Type)
		return
	}

	job.RecordError(err, attempt)

	if attempt >= maxAttempts {
		if err := w.enqueueDeadLetter(ctx, queue, job); err != nil {
			log.Printf("[Worker-%d] failed to enqueue job %s to dead-letter queue: %v", workerID, job.ID, err)
		}
		log.Printf("[Worker-%d] job %s (type: %s) failed after %d attempts: %v",
			workerID, job.ID, job.Type, attempt, err)
		return
	}

	if w.config.RetryOnErr {
		if err := w.enqueueDelayedRetry(ctx, queue, job); err != nil {
			log.Printf("[Worker-%d] failed to enqueue retry for job %s: %v", workerID, job.ID, err)
			return
		}
		log.Printf("[Worker-%d] job %s failed (attempt %d/%d): %v, queued for retry",
			workerID, job.ID, attempt, maxAttempts, err)
	}
}

// promoteDelayed moves ready jobs from the delayed queue back to the main queue.
func (w *Worker) promoteDelayed(ctx context.Context, jobType string) {
	defer w.wg.Done()

	queue, ok := w.registry.GetQueue(jobType)
	if !ok {
		log.Printf("[Promoter/%s] queue not found", jobType)
		return
	}

	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			log.Printf("[Promoter/%s] stopping", jobType)
			return
		case <-ctx.Done():
			log.Printf("[Promoter/%s] context cancelled", jobType)
			return
		case <-ticker.C:
			if err := w.promoteReadyJobs(ctx, queue); err != nil {
				log.Printf("[Promoter/%s] error promoting delayed jobs: %v", jobType, err)
			}
		}
	}
}

// promoteReadyJobs moves ready jobs from delayed queue to main queue.
func (w *Worker) promoteReadyJobs(ctx context.Context, queue *JobQueue) error {
	key := w.delayedQueueName(queue.GetQueueName())
	now := time.Now().UnixNano()

	batchSize := w.config.DelayBatchSize
	if batchSize < 1 {
		batchSize = 50
	}

	members, err := queue.RangeByScore(ctx, key, "-inf", fmt.Sprintf("%d", now), batchSize)
	if err != nil {
		return err
	}

	for _, member := range members {
		if err := queue.RemoveFromSet(ctx, key, member); err != nil {
			log.Printf("[Promoter/%s] failed to remove delayed job: %v", queue.GetJobType(), err)
			continue
		}
		if err := queue.Enqueue(ctx, member); err != nil {
			log.Printf("[Promoter/%s] failed to requeue delayed job: %v", queue.GetJobType(), err)
			continue
		}
	}

	return nil
}

// enqueueDelayedRetry adds a job to the delayed retry sorted set.
func (w *Worker) enqueueDelayedRetry(ctx context.Context, queue *JobQueue, job *Job) error {
	key := w.delayedQueueName(queue.GetQueueName())
	executeAt := time.Now().Add(w.config.RetryDelay).UnixNano()

	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal job for retry: %w", err)
	}

	return queue.AddToSet(ctx, key, float64(executeAt), string(payload))
}

// enqueueDeadLetter stores a failed job in the dead-letter queue.
func (w *Worker) enqueueDeadLetter(ctx context.Context, queue *JobQueue, job *Job) error {
	key := w.deadLetterQueueName(queue.GetQueueName())
	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal dead-letter job: %w", err)
	}
	return queue.GetClient().RPush(ctx, key, payload).Err()
}

func (w *Worker) delayedQueueName(queueName string) string {
	return fmt.Sprintf("%s:delayed", queueName)
}

func (w *Worker) deadLetterQueueName(queueName string) string {
	return fmt.Sprintf("%s:dead", queueName)
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
