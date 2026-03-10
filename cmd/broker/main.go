package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	redisqueue "github.com/umakantv/redis-queue/redisqueue"

	"github.com/redis/go-redis/v9"
)

func main() {
	// Broker config flags
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

	// Create registry
	registry := redisqueue.NewRegistry(client)

	// Register all known job types to handle their promotions
	// Handlers are not needed for promotion tasks
	registry.RegisterFunc("email", "queue:email", nil)
	registry.RegisterFunc("download", "queue:download", nil)
	registry.RegisterWithVisibility("prepare-report", "queue:prepare-report", nil, 60*time.Second)

	// Create a worker instance just for its promotion capabilities
	config := redisqueue.WorkerConfig{
		PollInterval: *pollInterval,
		RetryDelay:   *retryDelay,
	}
	broker := redisqueue.NewWorkerWithConfig(registry, config)

	log.Printf("Queue Broker started for all job types: %v", registry.RegisteredTypes())
	log.Printf("Config: poll-interval=%v, retry-delay=%v",
		config.PollInterval, config.RetryDelay)


	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start only the promotion background tasks
	ctx, cancel = context.WithCancel(context.Background())
	
	// Start promotion goroutines for each registered type
	for _, jt := range registry.RegisteredTypes() {
		broker.StartPromoters(ctx, jt)
	}

	// Wait for shutdown signal
	<-sigCh
	log.Println("Shutting down broker...")
	cancel()
	broker.Stop()
	log.Println("Broker stopped.")
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
