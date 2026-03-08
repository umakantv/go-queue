package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	redisqueue "github.com/umakantv/redis-queue/redisqueue"

	"github.com/redis/go-redis/v9"
)

// ReportPayload represents the payload for a prepare-report job.
type ReportPayload struct {
	ReportType string `json:"report_type"`
	StartDate  string `json:"start_date"`
	EndDate    string `json:"end_date"`
}

func main() {
	// Worker config flags
	concurrency := flag.Int("concurrency", 1, "Number of concurrent workers")
	pollInterval := flag.Duration("poll-interval", 100*time.Millisecond, "Interval between polling attempts")
	retry := flag.Bool("retry", false, "Enable retry for failed jobs")
	retryDelay := flag.Duration("retry-delay", 1*time.Second, "Delay before retrying a failed job")
	flag.Parse()


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

	// Create registry and register prepare-report handler
	registry := redisqueue.NewRegistry(client)

	err = registry.RegisterWithVisibility("prepare-report", "queue:prepare-report", redisqueue.HandlerFunc(func(ctx context.Context, job *redisqueue.Job) error {
		var payload ReportPayload
		if err := job.ParsePayload(&payload); err != nil {
			return fmt.Errorf("failed to parse report payload: %w", err)
		}

		log.Printf("Processing prepare-report job %s: report_type=%s, start_date=%s, end_date=%s", job.ID, payload.ReportType, payload.StartDate, payload.EndDate)

		// Simulate long-running report generation
		// Normal: 20-40 seconds
		// If report_type contains "[60s]": 80 seconds
		duration := time.Duration(20+rand.Intn(21)) * time.Second
		if contains(payload.ReportType, "[60s]") {
			duration = 80 * time.Second
			log.Printf("Job %s detected as long-running simulation ([60s]), duration set to %v", job.ID, duration)
		}
		log.Printf("Report generation started for job %s, estimated duration: %v", job.ID, duration)


		// Simulate the work with periodic progress updates
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		startTime := time.Now()
		done := make(chan bool)

		// Run the actual work in a goroutine
		go func() {
			time.Sleep(duration)
			done <- true
		}()

		// Monitor progress
		for {
			select {
			case <-ctx.Done():
				log.Printf("Report generation for job %s was cancelled", job.ID)
				return ctx.Err()
			case <-done:
				elapsed := time.Since(startTime)
				log.Printf("Report generated successfully: job_id=%s, report_type=%s, duration=%v", job.ID, payload.ReportType, elapsed)
				return nil
			case <-ticker.C:
				elapsed := time.Since(startTime)
				progress := float64(elapsed) / float64(duration) * 100
				log.Printf("Report generation progress for job %s: %.1f%% (%v elapsed)", job.ID, progress, elapsed)
			}
		}
	}), 60*time.Second)

	if err != nil {
		log.Fatalf("Failed to register prepare-report handler: %v", err)
	}

	// Create worker with config
	config := redisqueue.WorkerConfig{
		Concurrency:  *concurrency,
		PollInterval: *pollInterval,
		RetryOnErr:   *retry,
		RetryDelay:   *retryDelay,
	}
	worker := redisqueue.NewWorkerWithConfig(registry, config)

	log.Printf("Prepare-report worker started with config: concurrency=%d, poll-interval=%v, retry=%v, retry-delay=%v",
		config.Concurrency, config.PollInterval, config.RetryOnErr, config.RetryDelay)

	log.Println("Waiting for jobs...")

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start worker in a goroutine
	ctx, cancel = context.WithCancel(context.Background())
	go worker.Start(ctx, "prepare-report")

	// Wait for shutdown signal
	<-sigCh
	log.Println("Shutting down worker...")
	cancel()
	worker.Stop()
	log.Println("Worker stopped.")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsInMiddle(s, substr))
}

func containsInMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func init() {
	rand.Seed(time.Now().UnixNano())
}
