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
	Name      string `json:"name"`
	JobType   string `json:"job_type"`
	Size      int64  `json:"size"`
	QueueName string `json:"queue_name"`
}

// JobInfo represents job information for display.
type JobInfo struct {
	ID      string                 `json:"id"`
	Type    string                 `json:"type"`
	Payload map[string]interface{} `json:"payload"`
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
			ID:      job.ID,
			Type:    job.Type,
			Payload: payload,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jobInfos)
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

		stats = append(stats, QueueStats{
			Name:      jt,
			JobType:   jt,
			Size:      size,
			QueueName: queue.GetQueueName(),
		})
	}

	return stats
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}