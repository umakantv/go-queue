package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
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

func main() {
	// Worker config flags
	concurrency := flag.Int("concurrency", 1, "Number of concurrent workers")
	pollInterval := flag.Duration("poll-interval", 100*time.Millisecond, "Interval between polling attempts")
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

	// Create registry and register email handler
	registry := redisqueue.NewRegistry(client)

	err = registry.RegisterFunc("email", "queue:email", func(ctx context.Context, job *redisqueue.Job) error {
		var payload EmailPayload
		if err := job.ParsePayload(&payload); err != nil {
			return fmt.Errorf("failed to parse email payload: %w", err)
		}

		log.Printf("Processing email job %s: to=%s, subject=%s", job.ID, payload.To, payload.Subject)

		// Simulate sending email
		time.Sleep(500 * time.Millisecond)

		// Simulate error if email contains "error" - for testing retry logic
		if strings.Contains(strings.ToLower(payload.To), "error") {
			log.Printf("Simulated error for email job %s: invalid email address %s", job.ID, payload.To)
			return fmt.Errorf("failed to send email: invalid email address %s", payload.To)
		}

		log.Printf("Email sent successfully: job_id=%s, to=%s", job.ID, payload.To)
		return nil
	})
	if err != nil {
		log.Fatalf("Failed to register email handler: %v", err)
	}

	// Create worker with config
	config := redisqueue.WorkerConfig{
		Concurrency:  *concurrency,
		PollInterval: *pollInterval,
		RetryDelay:   *retryDelay,
	}
	worker := redisqueue.NewWorkerWithConfig(registry, config)

	log.Printf("Email worker started with config: concurrency=%d, poll-interval=%v, retry-delay=%v",
		config.Concurrency, config.PollInterval, config.RetryDelay)

	log.Println("Waiting for jobs...")

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start worker in a goroutine
	ctx, cancel = context.WithCancel(context.Background())
	go worker.Start(ctx, "email")

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