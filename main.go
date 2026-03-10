package main

import (
	"context"
	"fmt"
	"log"
	"time"

	redisqueue "github.com/umakantv/redis-queue/redisqueue"

	"github.com/redis/go-redis/v9"
)

// EmailPayload represents the payload for an email job.
type EmailPayload struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// DownloadPayload represents the payload for a download job.
type DownloadPayload struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
}

func main() {
	// Create a Redis client
	client := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379", // Redis server address
		Password: "",               // No password
		DB:       0,                // Default DB
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	fmt.Println("Successfully connected to Redis")

	// Create a new registry
	registry := redisqueue.NewRegistry(client)

	// Register job types with their handlers
	fmt.Println("\n--- Registering job types ---")

	// Register email job type
	err = registry.RegisterFunc("email", "queue:email", func(ctx context.Context, job *redisqueue.Job) error {
		var payload EmailPayload
		if err := job.ParsePayload(&payload); err != nil {
			return fmt.Errorf("failed to parse email payload: %w", err)
		}
		fmt.Printf("  [EMAIL] Sending email to %s with subject: %s\n", payload.To, payload.Subject)
		// Simulate sending email
		time.Sleep(100 * time.Millisecond)
		fmt.Printf("  [EMAIL] Email sent successfully to %s\n", payload.To)
		return nil
	})
	if err != nil {
		log.Fatalf("Failed to register email job type: %v", err)
	}
	fmt.Println("  Registered job type: email")

	// Register download job type
	err = registry.RegisterFunc("download", "queue:download", func(ctx context.Context, job *redisqueue.Job) error {
		var payload DownloadPayload
		if err := job.ParsePayload(&payload); err != nil {
			return fmt.Errorf("failed to parse download payload: %w", err)
		}
		fmt.Printf("  [DOWNLOAD] Downloading %s as %s\n", payload.URL, payload.Filename)
		// Simulate downloading
		time.Sleep(200 * time.Millisecond)
		fmt.Printf("  [DOWNLOAD] Downloaded %s successfully\n", payload.Filename)
		return nil
	})
	if err != nil {
		log.Fatalf("Failed to register download job type: %v", err)
	}
	fmt.Println("  Registered job type: download")

	// List registered job types
	fmt.Printf("\nRegistered job types: %v\n", registry.RegisteredTypes())

	// Clear queues for a fresh start
	if queue, ok := registry.GetQueue("email"); ok {
		queue.Clear(ctx)
	}
	if queue, ok := registry.GetQueue("download"); ok {
		queue.Clear(ctx)
	}

	// Enqueue jobs
	fmt.Println("\n--- Enqueueing jobs ---")

	// Enqueue email jobs
	emailJobs := []EmailPayload{
		{To: "user1@example.com", Subject: "Welcome!", Body: "Welcome to our service!"},
		{To: "user2@example.com", Subject: "Order Confirmation", Body: "Your order #12345 is confirmed."},
	}
	for _, ep := range emailJobs {
		job, err := registry.Enqueue(ctx, "email", ep, 0, 0) // maxRetries=0, priority=0 (uses default)
		if err != nil {
			log.Printf("Failed to enqueue email job: %v", err)
		} else {
			fmt.Printf("  Enqueued email job %s to %s (priority: %d)\n", job.ID, ep.To, job.Priority)
		}
	}

	// Enqueue download jobs
	downloadJobs := []DownloadPayload{
		{URL: "https://example.com/file1.pdf", Filename: "file1.pdf"},
		{URL: "https://example.com/file2.zip", Filename: "file2.zip"},
	}
	for _, dp := range downloadJobs {
		job, err := registry.Enqueue(ctx, "download", dp, 0, 0) // maxRetries=0, priority=0 (uses default)
		if err != nil {
			log.Printf("Failed to enqueue download job: %v", err)
		} else {
			fmt.Printf("  Enqueued download job %s for %s (priority: %d)\n", job.ID, dp.Filename, job.Priority)
		}
	}

	// Check queue sizes
	fmt.Println("\n--- Queue sizes ---")
	if queue, ok := registry.GetQueue("email"); ok {
		size, _ := queue.Size(ctx)
		fmt.Printf("  Email queue: %d jobs\n", size)
	}
	if queue, ok := registry.GetQueue("download"); ok {
		size, _ := queue.Size(ctx)
		fmt.Printf("  Download queue: %d jobs\n", size)
	}

	// Process all jobs using a worker
	fmt.Println("\n--- Processing jobs ---")
	worker := redisqueue.NewWorker(registry)
	if err := worker.ProcessAll(ctx); err != nil {
		log.Printf("Error processing jobs: %v", err)
	}

	// Verify queues are empty
	fmt.Println("\n--- Final queue sizes ---")
	if queue, ok := registry.GetQueue("email"); ok {
		size, _ := queue.Size(ctx)
		fmt.Printf("  Email queue: %d jobs\n", size)
	}
	if queue, ok := registry.GetQueue("download"); ok {
		size, _ := queue.Size(ctx)
		fmt.Printf("  Download queue: %d jobs\n", size)
	}

	fmt.Println("\nDone!")
}

