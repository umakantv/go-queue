package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	redisqueue "github.com/umakantv/redis-queue/redisqueue"

	"github.com/redis/go-redis/v9"
)

// DownloadPayload represents the payload for a download job.
type DownloadPayload struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
}

func main() {
	// Worker config flags
	concurrency := flag.Int("concurrency", 1, "Number of concurrent workers")
	pollInterval := flag.Duration("poll-interval", 100*time.Millisecond, "Interval between polling attempts")
	retry := flag.Bool("retry", false, "Enable retry for failed jobs")
	retryDelay := flag.Duration("retry-delay", 1*time.Second, "Delay before retrying a failed job")
	maxRetries := flag.Int("max-retries", 3, "Maximum number of total attempts (including initial attempt)")
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

	// Create registry and register download handler
	registry := redisqueue.NewRegistry(client)

	err = registry.RegisterFunc("download", "queue:download", func(ctx context.Context, job *redisqueue.Job) error {
		var payload DownloadPayload
		if err := job.ParsePayload(&payload); err != nil {
			return fmt.Errorf("failed to parse download payload: %w", err)
		}

		log.Printf("Processing download job %s: url=%s, filename=%s", job.ID, payload.URL, payload.Filename)

		// Simulate downloading file
		time.Sleep(1 * time.Second)

		log.Printf("Download completed: job_id=%s, filename=%s", job.ID, payload.Filename)
		return nil
	})
	if err != nil {
		log.Fatalf("Failed to register download handler: %v", err)
	}

	// Create worker with config
	config := redisqueue.WorkerConfig{
		Concurrency:  *concurrency,
		PollInterval: *pollInterval,
		RetryOnErr:   *retry,
		RetryDelay:   *retryDelay,
		MaxRetries:   *maxRetries,
	}
	worker := redisqueue.NewWorkerWithConfig(registry, config)

	log.Printf("Download worker started with config: concurrency=%d, poll-interval=%v, retry=%v, retry-delay=%v, max-retries=%d",
		config.Concurrency, config.PollInterval, config.RetryOnErr, config.RetryDelay, config.MaxRetries)
	log.Println("Waiting for jobs...")

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start worker in a goroutine
	ctx, cancel = context.WithCancel(context.Background())
	go worker.Start(ctx, "download")

	// Wait for shutdown signal
	<-sigCh
	log.Println("Shutting down worker...")
	cancel()
	worker.Stop()
	log.Println("Worker stopped.")
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}