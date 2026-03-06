package redisqueue

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// Job represents a unit of work to be processed.
type Job struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// NewJob creates a new Job with a unique ID.
func NewJob(jobType string, payload any) (*Job, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal job payload: %w", err)
	}
	return &Job{
		ID:      uuid.New().String(),
		Type:    jobType,
		Payload: payloadBytes,
	}, nil
}

// ParsePayload unmarshals the job payload into the provided destination.
func (j *Job) ParsePayload(dest any) error {
	if err := json.Unmarshal(j.Payload, dest); err != nil {
		return fmt.Errorf("failed to unmarshal job payload: %w", err)
	}
	return nil
}

// Handler defines the interface for processing jobs.
type Handler interface {
	Handle(ctx context.Context, job *Job) error
}

// HandlerFunc is an adapter to allow using functions as handlers.
type HandlerFunc func(ctx context.Context, job *Job) error

// Handle implements the Handler interface.
func (h HandlerFunc) Handle(ctx context.Context, job *Job) error {
	return h(ctx, job)
}

// JobQueue wraps RedisQueue with job-specific functionality.
type JobQueue struct {
	*RedisQueue
	jobType string
	handler Handler
}

// NewJobQueue creates a new JobQueue for a specific job type.
func NewJobQueue(client *RedisQueue, jobType string, handler Handler) *JobQueue {
	return &JobQueue{
		RedisQueue: client,
		jobType:    jobType,
		handler:    handler,
	}
}

// EnqueueJob serializes and adds a job to the queue.
func (q *JobQueue) EnqueueJob(ctx context.Context, job *Job) error {
	jobBytes, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal job: %w", err)
	}
	return q.Enqueue(ctx, string(jobBytes))
}

// DequeueJob removes and deserializes a job from the queue.
func (q *JobQueue) DequeueJob(ctx context.Context) (*Job, error) {
	data, err := q.Dequeue(ctx)
	if err != nil {
		return nil, err
	}
	var job Job
	if err := json.Unmarshal([]byte(data), &job); err != nil {
		return nil, fmt.Errorf("failed to unmarshal job: %w", err)
	}
	return &job, nil
}

// PeekJobs returns jobs from the queue without removing them.
// start and count are 0-based. Use -1 for count to get all jobs.
func (q *JobQueue) PeekJobs(ctx context.Context, start, count int64) ([]*Job, error) {
	var stop int64
	if count < 0 {
		stop = -1
	} else {
		stop = start + count - 1
	}
	data, err := q.Peek(ctx, start, stop)
	if err != nil {
		return nil, err
	}
	jobs := make([]*Job, 0, len(data))
	for _, d := range data {
		var job Job
		if err := json.Unmarshal([]byte(d), &job); err != nil {
			continue // skip invalid jobs
		}
		jobs = append(jobs, &job)
	}
	return jobs, nil
}

// Process dequeues a single job and executes its handler.
func (q *JobQueue) Process(ctx context.Context) error {
	job, err := q.DequeueJob(ctx)
	if err != nil {
		return err
	}
	return q.handler.Handle(ctx, job)
}

// GetJobType returns the job type this queue handles.
func (q *JobQueue) GetJobType() string {
	return q.jobType
}

// GetHandler returns the handler for this queue.
func (q *JobQueue) GetHandler() Handler {
	return q.handler
}
