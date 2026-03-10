package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	redisqueue "github.com/umakantv/redis-queue/redisqueue"

	"github.com/redis/go-redis/v9"
)

//go:embed templates/*
var templateFS embed.FS

// QueueStats represents statistics for a queue.
type QueueStats struct {
	Name           string `json:"name"`
	JobType        string `json:"job_type"`
	Size           int64  `json:"size"`
	QueueName      string `json:"queue_name"`
	DelayedSize    int64  `json:"delayed_size"`
	ScheduledSize  int64  `json:"scheduled_size"`
	ProcessingSize int64  `json:"processing_size"`
	DeadLetterSize int64  `json:"dead_letter_size"`
}

// JobInfo represents job information for display.
type JobInfo struct {
	ID         string                   `json:"id"`
	Type       string                   `json:"type"`
	Payload    map[string]interface{}   `json:"payload"`
	RetryCount int                      `json:"retry_count,omitempty"`
	Priority   int                      `json:"priority"`
	Errors     []redisqueue.JobError    `json:"errors,omitempty"`
}

// DeadLetterJobInfo represents a job in the dead-letter queue.
type DeadLetterJobInfo struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	Payload    map[string]interface{} `json:"payload"`
	RetryCount int                    `json:"retry_count"`
	Priority   int                    `json:"priority"`
	Errors     []redisqueue.JobError  `json:"errors"`
	QueueName  string                 `json:"queue_name"`
}

// QueueUpdate represents an update sent via SSE.
type QueueUpdate struct {
	Queues []QueueStats `json:"queues"`
	Jobs   []JobInfo    `json:"jobs,omitempty"`
}

// Dashboard holds the dashboard state.
type Dashboard struct {
	registry *redisqueue.Registry
	client   *redis.Client
	tmpl     *template.Template

	// SSE clients management
	clientsMu sync.Mutex
	clients   map[chan QueueUpdate]struct{}
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Create a Redis client
	client := redis.NewClient(&redis.Options{
		Addr:     getEnv("REDIS_ADDR", "localhost:6379"),
		Password: getEnv("REDIS_PASSWORD", ""),
		DB:       0,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_, err := client.Ping(ctx).Result()
	cancel()
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Println("Connected to Redis")

	// Create registry
	registry := redisqueue.NewRegistry(client)

	// Register known job types (without handlers, just for stats)
	registry.RegisterFunc("email", "queue:email", func(ctx context.Context, job *redisqueue.Job) error {
		return nil
	})
	registry.RegisterFunc("download", "queue:download", func(ctx context.Context, job *redisqueue.Job) error {
		return nil
	})
	registry.RegisterWithVisibility("prepare-report", "queue:prepare-report", redisqueue.HandlerFunc(func(ctx context.Context, job *redisqueue.Job) error {
		return nil
	}), 60*time.Second)


	// Parse templates
	tmpl, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		log.Fatalf("Failed to parse templates: %v", err)
	}

	dashboard := &Dashboard{
		registry: registry,
		client:   client,
		tmpl:     tmpl,
		clients:  make(map[chan QueueUpdate]struct{}),
	}

	// Start background updater
	go dashboard.broadcastUpdates()

	// Set up routes
	http.HandleFunc("/", dashboard.handleIndex)
	http.HandleFunc("/api/queues", dashboard.handleQueuesAPI)
	http.HandleFunc("/api/queue/", dashboard.handleQueueJobsAPI)
	http.HandleFunc("/api/jobs", dashboard.handleCreateJobAPI)
	http.HandleFunc("/api/dead-letter/", dashboard.handleDeadLetterAPI)
	http.HandleFunc("/api/replay-job/", dashboard.handleReplayJobAPI)
	http.HandleFunc("/api/delayed/", dashboard.handleDelayedAPI)
	http.HandleFunc("/api/scheduled/", dashboard.handleScheduledAPI)
	http.HandleFunc("/api/scheduled-delete/", dashboard.handleScheduledDeleteAPI)
	http.HandleFunc("/api/processing/", dashboard.handleProcessingAPI)
	http.HandleFunc("/events", dashboard.handleSSE)

	addr := ":" + port
	log.Printf("Dashboard starting on http://localhost%s", addr)

	// Handle graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-stop
	log.Println("Shutting down dashboard...")
}

func (d *Dashboard) handleIndex(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stats := d.getQueueStats(ctx)

	data := struct {
		Queues []QueueStats
	}{
		Queues: stats,
	}

	if err := d.tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (d *Dashboard) handleQueuesAPI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stats := d.getQueueStats(ctx)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func (d *Dashboard) handleQueueJobsAPI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract job type from URL path
	jobType := r.URL.Path[len("/api/queue/"):]
	if jobType == "" {
		http.Error(w, "job type required", http.StatusBadRequest)
		return
	}

	queue, ok := d.registry.GetQueue(jobType)
	if !ok {
		http.Error(w, "queue not found", http.StatusNotFound)
		return
	}

	// Get jobs from queue (peek without removing)
	jobs, err := queue.PeekJobs(ctx, 0, 100)
	if err != nil && err != redis.Nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert to JobInfo for display
	jobInfos := make([]JobInfo, 0, len(jobs))
	for _, job := range jobs {
		var payload map[string]interface{}
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			payload = map[string]interface{}{"raw": string(job.Payload)}
		}
		jobInfos = append(jobInfos, JobInfo{
			ID:         job.ID,
			Type:       job.Type,
			Payload:    payload,
			RetryCount: job.RetryCount,
			Priority:   job.Priority,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jobInfos)
}

// CreateJobRequest represents the request body for creating a job.
type CreateJobRequest struct {
	Queue      string                 `json:"queue"`            // Queue name (job type)
	ID         string                 `json:"id,omitempty"`     // Optional job ID (generated if not provided)
	MaxRetries int                    `json:"max_retries"`      // Optional max retries
	Priority   int                    `json:"priority"`         // Optional priority (default: 3, lower = higher priority)
	StartAt    string                 `json:"start_at"`         // Optional scheduled execution time (RFC3339 format)
	Payload    map[string]interface{} `json:"payload"`          // Job payload
}


// CreateJobResponse represents the response for a created job.
type CreateJobResponse struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Queue    string                 `json:"queue"`
	Priority int                    `json:"priority"`
	StartAt  string                 `json:"start_at,omitempty"`
	Payload  map[string]interface{} `json:"payload"`
}

// handleCreateJobAPI handles POST requests to create a new job.
func (d *Dashboard) handleCreateJobAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.Queue == "" {
		http.Error(w, "queue is required", http.StatusBadRequest)
		return
	}
	if req.Payload == nil {
		http.Error(w, "payload is required", http.StatusBadRequest)
		return
	}

	// Check if queue/job type is registered
	queue, ok := d.registry.GetQueue(req.Queue)
	if !ok {
		http.Error(w, "queue not found: "+req.Queue, http.StatusNotFound)
		return
	}

	ctx := r.Context()

	// Create job with provided or generated ID
	job := &redisqueue.Job{
		Type:       req.Queue,
		MaxRetries: req.MaxRetries,
		Priority:   req.Priority,
		StartAt:    req.StartAt,
	}

	// Use default priority if not specified or invalid
	if job.Priority <= 0 {
		job.Priority = redisqueue.DefaultPriority
	}

	// Use provided ID or generate a new one
	if req.ID != "" {
		job.ID = req.ID
	} else {
		job.ID = generateJobID()
	}

	// Marshal payload
	payloadBytes, err := json.Marshal(req.Payload)
	if err != nil {
		http.Error(w, "Failed to marshal payload: "+err.Error(), http.StatusInternalServerError)
		return
	}
	job.Payload = payloadBytes

	// Enqueue the job
	if err := queue.EnqueueJob(ctx, job); err != nil {
		http.Error(w, "Failed to enqueue job: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Return the created job
	response := CreateJobResponse{
		ID:       job.ID,
		Type:     job.Type,
		Queue:    queue.GetQueueName(),
		Priority: job.Priority,
		StartAt:  job.StartAt,
		Payload:  req.Payload,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// generateJobID generates a unique job ID.
func generateJobID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// handleSSE handles Server-Sent Events connections.
func (d *Dashboard) handleSSE(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Create client channel
	clientChan := make(chan QueueUpdate, 10)
	d.addClient(clientChan)
	defer d.removeClient(clientChan)

	// Send initial data
	ctx := r.Context()
	stats := d.getQueueStats(ctx)
	initialUpdate := QueueUpdate{Queues: stats}
	d.sendSSEEvent(w, initialUpdate)

	// Flush if supported
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Listen for client disconnect or updates
	for {
		select {
		case <-ctx.Done():
			return
		case update := <-clientChan:
			if err := d.sendSSEEvent(w, update); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}
}

func (d *Dashboard) sendSSEEvent(w http.ResponseWriter, update QueueUpdate) error {
	data, err := json.Marshal(update)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

func (d *Dashboard) addClient(clientChan chan QueueUpdate) {
	d.clientsMu.Lock()
	defer d.clientsMu.Unlock()
	d.clients[clientChan] = struct{}{}
}

func (d *Dashboard) removeClient(clientChan chan QueueUpdate) {
	d.clientsMu.Lock()
	defer d.clientsMu.Unlock()
	delete(d.clients, clientChan)
	close(clientChan)
}

// broadcastUpdates periodically broadcasts queue updates to all SSE clients.
func (d *Dashboard) broadcastUpdates() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		ctx := context.Background()
		stats := d.getQueueStats(ctx)

		update := QueueUpdate{Queues: stats}

		d.clientsMu.Lock()
		for clientChan := range d.clients {
			select {
			case clientChan <- update:
			default:
				// Client channel full, skip
			}
		}
		d.clientsMu.Unlock()
	}
}

func (d *Dashboard) getQueueStats(ctx context.Context) []QueueStats {
	types := d.registry.RegisteredTypes()
	stats := make([]QueueStats, 0, len(types))

	for _, jt := range types {
		queue, ok := d.registry.GetQueue(jt)
		if !ok {
			continue
		}

		size, err := queue.Size(ctx)
		if err != nil {
			size = -1
		}

		// Get delayed queue size
		delayedKey := fmt.Sprintf("%s:delayed", queue.GetQueueName())
		delayedSize, _ := d.client.ZCard(ctx, delayedKey).Result()

		// Get scheduled queue size
		scheduledKey := queue.ScheduledQueueName()
		scheduledSize, _ := d.client.ZCard(ctx, scheduledKey).Result()

		// Get processing queue size
		processingKey := queue.GetProcessingKey()
		processingSize, _ := d.client.ZCard(ctx, processingKey).Result()

		// Get dead-letter queue size
		deadKey := fmt.Sprintf("%s:dead", queue.GetQueueName())
		deadSize, _ := d.client.LLen(ctx, deadKey).Result()

		stats = append(stats, QueueStats{
			Name:           jt,
			JobType:        jt,
			Size:           size,
			QueueName:      queue.GetQueueName(),
			DelayedSize:    delayedSize,
			ScheduledSize:  scheduledSize,
			ProcessingSize: processingSize,
			DeadLetterSize: deadSize,
		})
	}

	return stats
}

// handleDeadLetterAPI handles requests for dead-letter queue operations.
// GET /api/dead-letter/{jobType} - list dead-letter jobs
// DELETE /api/dead-letter/{jobType} - clear dead-letter queue
func (d *Dashboard) handleDeadLetterAPI(w http.ResponseWriter, r *http.Request) {
	// Extract job type from URL path
	path := r.URL.Path[len("/api/dead-letter/"):]
	if path == "" {
		http.Error(w, "job type required", http.StatusBadRequest)
		return
	}

	// Split path to get job type and optional job ID
	parts := splitPath(path)
	if len(parts) == 0 {
		http.Error(w, "job type required", http.StatusBadRequest)
		return
	}

	jobType := parts[0]
	queue, ok := d.registry.GetQueue(jobType)
	if !ok {
		http.Error(w, "queue not found", http.StatusNotFound)
		return
	}

	deadKey := fmt.Sprintf("%s:dead", queue.GetQueueName())
	ctx := r.Context()

	switch r.Method {
	case http.MethodGet:
		// List dead-letter jobs
		jobs, err := d.client.LRange(ctx, deadKey, 0, 100).Result()
		if err != nil && err != redis.Nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		deadJobs := make([]DeadLetterJobInfo, 0, len(jobs))
		for _, jobStr := range jobs {
			var job redisqueue.Job
			if err := json.Unmarshal([]byte(jobStr), &job); err != nil {
				continue
			}
			var payload map[string]interface{}
			json.Unmarshal(job.Payload, &payload)
			deadJobs = append(deadJobs, DeadLetterJobInfo{
				ID:         job.ID,
				Type:       job.Type,
				Payload:    payload,
				RetryCount: job.RetryCount,
				Priority:   job.Priority,
				Errors:     job.Errors,
				QueueName:  queue.GetQueueName(),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(deadJobs)

	case http.MethodDelete:
		// Clear dead-letter queue
		if err := d.client.Del(ctx, deadKey).Err(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "cleared"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleReplayJobAPI handles replaying a dead-letter job.
// POST /api/replay-job/{jobType}/{jobID}
func (d *Dashboard) handleReplayJobAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract job type and job ID from URL path
	path := r.URL.Path[len("/api/replay-job/"):]
	parts := splitPath(path)
	if len(parts) < 2 {
		http.Error(w, "job type and job ID required", http.StatusBadRequest)
		return
	}

	jobType := parts[0]
	jobID := parts[1]

	queue, ok := d.registry.GetQueue(jobType)
	if !ok {
		http.Error(w, "queue not found", http.StatusNotFound)
		return
	}

	ctx := r.Context()
	deadKey := fmt.Sprintf("%s:dead", queue.GetQueueName())

	// Find and remove the job from dead-letter queue
	jobs, err := d.client.LRange(ctx, deadKey, 0, -1).Result()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var foundJob *redisqueue.Job
	for _, jobStr := range jobs {
		var job redisqueue.Job
		if err := json.Unmarshal([]byte(jobStr), &job); err != nil {
			continue
		}
		if job.ID == jobID {
			foundJob = &job
			// Remove this specific job from dead-letter queue
			d.client.LRem(ctx, deadKey, 1, jobStr)
			break
		}
	}

	if foundJob == nil {
		http.Error(w, "job not found in dead-letter queue", http.StatusNotFound)
		return
	}

	// Reset retry count and errors for replay
	newJob := &redisqueue.Job{
		ID:      foundJob.ID,
		Type:    foundJob.Type,
		Payload: foundJob.Payload,
	}

	// Enqueue to main queue
	if err := queue.EnqueueJob(ctx, newJob); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "replayed",
		"job_id":  newJob.ID,
		"queue":   jobType,
	})
}

// handleDelayedAPI handles requests for delayed queue.
// GET /api/delayed/{jobType} - list delayed jobs
func (d *Dashboard) handleDelayedAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract job type from URL path
	jobType := r.URL.Path[len("/api/delayed/"):]
	if jobType == "" {
		http.Error(w, "job type required", http.StatusBadRequest)
		return
	}

	queue, ok := d.registry.GetQueue(jobType)
	if !ok {
		http.Error(w, "queue not found", http.StatusNotFound)
		return
	}

	ctx := r.Context()
	delayedKey := fmt.Sprintf("%s:delayed", queue.GetQueueName())

	// Get delayed jobs (with scores as timestamps)
	results, err := d.client.ZRangeWithScores(ctx, delayedKey, 0, 100).Result()
	if err != nil && err != redis.Nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type DelayedJobInfo struct {
		ID          string                 `json:"id"`
		Type        string                 `json:"type"`
		Payload     map[string]interface{} `json:"payload"`
		RetryCount  int                    `json:"retry_count"`
		Priority    int                    `json:"priority"`
		ExecuteAt   string                 `json:"execute_at"`
	}

	delayedJobs := make([]DelayedJobInfo, 0, len(results))
	for _, z := range results {
		var job redisqueue.Job
		if err := json.Unmarshal([]byte(z.Member.(string)), &job); err != nil {
			continue
		}
		var payload map[string]interface{}
		json.Unmarshal(job.Payload, &payload)
		executeAt := time.Unix(0, int64(z.Score)).UTC().Format(time.RFC3339)
		delayedJobs = append(delayedJobs, DelayedJobInfo{
			ID:         job.ID,
			Type:       job.Type,
			Payload:    payload,
			RetryCount: job.RetryCount,
			Priority:   job.Priority,
			ExecuteAt:  executeAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(delayedJobs)
}

// handleScheduledAPI handles requests for scheduled jobs.
// GET /api/scheduled/{jobType} - list scheduled jobs
func (d *Dashboard) handleScheduledAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract job type from URL path
	jobType := r.URL.Path[len("/api/scheduled/"):]
	if jobType == "" {
		http.Error(w, "job type required", http.StatusBadRequest)
		return
	}

	queue, ok := d.registry.GetQueue(jobType)
	if !ok {
		http.Error(w, "queue not found", http.StatusNotFound)
		return
	}

	ctx := r.Context()
	scheduledKey := queue.ScheduledQueueName()
	results, err := d.client.ZRangeWithScores(ctx, scheduledKey, 0, 100).Result()
	if err != nil && err != redis.Nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type ScheduledJobInfo struct {
		ID        string                 `json:"id"`
		Type      string                 `json:"type"`
		Payload   map[string]interface{} `json:"payload"`
		Priority  int                    `json:"priority"`
		StartAt   string                 `json:"start_at"`
		DelayMs   int64                  `json:"delay_ms"`
	}

	now := time.Now().UTC()
	scheduledJobs := make([]ScheduledJobInfo, 0, len(results))
	for _, z := range results {
		var job redisqueue.Job
		if err := json.Unmarshal([]byte(z.Member.(string)), &job); err != nil {
			continue
		}

		if job.StartAt == "" {
			continue
		}

		executeAt := time.Unix(0, int64(z.Score)).UTC()
		delayMs := executeAt.Sub(now).Milliseconds()
		if delayMs < 0 {
			delayMs = 0
		}

		var payload map[string]interface{}
		json.Unmarshal(job.Payload, &payload)

		scheduledJobs = append(scheduledJobs, ScheduledJobInfo{
			ID:       job.ID,
			Type:     job.Type,
			Payload:  payload,
			Priority: job.Priority,
			StartAt:  executeAt.Format(time.RFC3339),
			DelayMs:  delayMs,
		})
	}

	sort.Slice(scheduledJobs, func(i, j int) bool {
		return scheduledJobs[i].StartAt < scheduledJobs[j].StartAt
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(scheduledJobs)
}

// handleScheduledDeleteAPI handles deleting a scheduled job.
// DELETE /api/scheduled-delete/{jobType}/{jobID} - remove scheduled job by ID
func (d *Dashboard) handleScheduledDeleteAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/scheduled-delete/")
	if path == "" {
		http.Error(w, "job type and id required", http.StatusBadRequest)
		return
	}

	parts := splitPath(path)
	if len(parts) < 2 {
		http.Error(w, "job type and id required", http.StatusBadRequest)
		return
	}

	jobType := parts[0]
	jobID := parts[1]

	queue, ok := d.registry.GetQueue(jobType)
	if !ok {
		http.Error(w, "queue not found", http.StatusNotFound)
		return
	}

	ctx := r.Context()
	scheduledKey := queue.ScheduledQueueName()

	results, err := d.client.ZRange(ctx, scheduledKey, 0, -1).Result()
	if err != nil && err != redis.Nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for _, member := range results {
		var job redisqueue.Job
		if err := json.Unmarshal([]byte(member), &job); err != nil {
			continue
		}
		if job.ID == jobID {
			if err := queue.RemoveFromSet(ctx, scheduledKey, member); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"status": "deleted",
				"job_id": jobID,
			})
			return
		}
	}

	http.Error(w, "job not found", http.StatusNotFound)
}

// handleProcessingAPI handles requests for processing queue.
// GET /api/processing/{jobType} - list processing jobs
func (d *Dashboard) handleProcessingAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract job type from URL path
	jobType := r.URL.Path[len("/api/processing/"):]
	if jobType == "" {
		http.Error(w, "job type required", http.StatusBadRequest)
		return
	}

	queue, ok := d.registry.GetQueue(jobType)
	if !ok {
		http.Error(w, "queue not found", http.StatusNotFound)
		return
	}

	ctx := r.Context()
	processingKey := queue.GetProcessingKey()

	// Get processing jobs (with scores as timestamps)
	results, err := d.client.ZRangeWithScores(ctx, processingKey, 0, 100).Result()
	if err != nil && err != redis.Nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type ProcessingJobInfo struct {
		ID         string                 `json:"id"`
		Type       string                 `json:"type"`
		Payload    map[string]interface{} `json:"payload"`
		RetryCount int                    `json:"retry_count"`
		Priority   int                    `json:"priority"`
		VisibleAt  string                 `json:"visible_at"`
		PickedUpAt string                 `json:"picked_up_at"`
	}

	processingJobs := make([]ProcessingJobInfo, 0, len(results))
	for _, z := range results {
		var job redisqueue.Job
		if err := json.Unmarshal([]byte(z.Member.(string)), &job); err != nil {
			continue
		}
		var payload map[string]interface{}
		json.Unmarshal(job.Payload, &payload)
		visibleAt := time.Unix(0, int64(z.Score)).UTC().Format(time.RFC3339)
		processingJobs = append(processingJobs, ProcessingJobInfo{
			ID:         job.ID,
			Type:       job.Type,
			Payload:    payload,
			RetryCount: job.RetryCount,
			Priority:   job.Priority,
			VisibleAt:  visibleAt,
			PickedUpAt: job.PickedUpAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(processingJobs)
}

// splitPath splits a URL path into parts, removing empty strings.
func splitPath(path string) []string {
	parts := make([]string, 0)
	for _, p := range []byte(path) {
		if len(parts) == 0 {
			parts = append(parts, "")
		}
		if p == '/' {
			parts = append(parts, "")
		} else {
			parts[len(parts)-1] += string(p)
		}
	}
	// Filter empty strings
	result := make([]string, 0)
	for _, p := range parts {
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}