package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
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

var (
	emailSubjects = []string{
		"Welcome to our service!",
		"Your order has been confirmed",
		"Password reset request",
		"Monthly newsletter",
		"Account verification",
		"Special offer just for you!",
		"Your invoice is ready",
		"Meeting reminder",
	}

	emailBodies = []string{
		"Thank you for joining us!",
		"Your order #12345 has been confirmed and will ship soon.",
		"Click here to reset your password.",
		"Here's what's new this month...",
		"Please verify your email address.",
		"Get 20% off your next purchase!",
		"Your invoice for March 2024 is now available.",
		"Don't forget your meeting tomorrow at 10 AM.",
	}

	downloadURLs = []string{
		"https://example.com/files/report.pdf",
		"https://example.com/files/data.csv",
		"https://example.com/files/archive.zip",
		"https://example.com/files/image.png",
		"https://example.com/files/video.mp4",
		"https://example.com/files/audio.mp3",
		"https://example.com/files/document.docx",
		"https://example.com/files/spreadsheet.xlsx",
	}

	domains = []string{
		"gmail.com",
		"yahoo.com",
		"outlook.com",
		"example.com",
		"company.org",
	}
)

func main() {
	jobType := flag.String("type", "", "Job type: email or download (required)")
	count := flag.Int("count", 1, "Number of jobs to create")
	list := flag.Bool("list", false, "List pending jobs in queues")
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

	// Create registry
	registry := redisqueue.NewRegistry(client)

	// Register both job types (without handlers, just for enqueueing)
	registry.RegisterFunc("email", "queue:email", func(ctx context.Context, job *redisqueue.Job) error {
		return nil
	})
	registry.RegisterFunc("download", "queue:download", func(ctx context.Context, job *redisqueue.Job) error {
		return nil
	})

	// List mode
	if *list {
		listQueues(ctx, registry)
		return
	}

	// Validate job type
	if *jobType == "" {
		fmt.Println("Error: -type flag is required")
		fmt.Println("Usage:")
		fmt.Println("  producer -type email [-count N]    Create N email jobs")
		fmt.Println("  producer -type download [-count N] Create N download jobs")
		fmt.Println("  producer -list                     List pending jobs in queues")
		os.Exit(1)
	}

	// Seed random
	rand.Seed(time.Now().UnixNano())

	// Create jobs
	ctx = context.Background()
	switch *jobType {
	case "email":
		for i := 0; i < *count; i++ {
			payload := generateEmailPayload()
			job, err := registry.Enqueue(ctx, "email", payload)
			if err != nil {
				log.Printf("Failed to enqueue email job: %v", err)
				continue
			}
			fmt.Printf("Created email job %s: to=%s, subject=%s\n", job.ID, payload.To, payload.Subject)
		}
		fmt.Printf("\nCreated %d email job(s)\n", *count)

	case "download":
		for i := 0; i < *count; i++ {
			payload := generateDownloadPayload()
			job, err := registry.Enqueue(ctx, "download", payload)
			if err != nil {
				log.Printf("Failed to enqueue download job: %v", err)
				continue
			}
			fmt.Printf("Created download job %s: url=%s, filename=%s\n", job.ID, payload.URL, payload.Filename)
		}
		fmt.Printf("\nCreated %d download job(s)\n", *count)

	default:
		log.Fatalf("Unknown job type: %s (use 'email' or 'download')", *jobType)
	}
}

func generateEmailPayload() EmailPayload {
	subjectIdx := rand.Intn(len(emailSubjects))
	email := fmt.Sprintf("user%d@%s", rand.Intn(1000), domains[rand.Intn(len(domains))])
	
	// Add "error" to roughly 2 out of 20 emails (10% chance)
	// This inserts "error" into the email address for testing retry logic
	if rand.Intn(20) < 2 {
		// Insert "error" before the @ symbol
		email = fmt.Sprintf("user%derror@%s", rand.Intn(1000), domains[rand.Intn(len(domains))])
	}
	
	return EmailPayload{
		To:      email,
		Subject: emailSubjects[subjectIdx],
		Body:    emailBodies[subjectIdx],
	}
}

func generateDownloadPayload() DownloadPayload {
	urlIdx := rand.Intn(len(downloadURLs))
	url := downloadURLs[urlIdx]
	// Extract filename from URL
	filename := fmt.Sprintf("file_%d%s", rand.Intn(10000), getExtension(url))
	return DownloadPayload{
		URL:      url,
		Filename: filename,
	}
}

func getExtension(url string) string {
	for i := len(url) - 1; i >= 0; i-- {
		if url[i] == '.' {
			return url[i:]
		}
	}
	return ""
}

func listQueues(ctx context.Context, registry *redisqueue.Registry) {
	fmt.Println("Queue Status:")
	fmt.Println("=============")

	if queue, ok := registry.GetQueue("email"); ok {
		size, err := queue.Size(ctx)
		if err != nil {
			fmt.Printf("  Email queue: error getting size: %v\n", err)
		} else {
			fmt.Printf("  Email queue: %d pending jobs\n", size)
		}
	}

	if queue, ok := registry.GetQueue("download"); ok {
		size, err := queue.Size(ctx)
		if err != nil {
			fmt.Printf("  Download queue: error getting size: %v\n", err)
		} else {
			fmt.Printf("  Download queue: %d pending jobs\n", size)
		}
	}
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

// Helper to pretty print job details
func printJobDetails(job *redisqueue.Job) {
	var payload map[string]interface{}
	if err := json.Unmarshal(job.Payload, &payload); err == nil {
		data, _ := json.MarshalIndent(payload, "  ", "  ")
		fmt.Printf("  Job ID: %s\n  Type: %s\n  Payload:\n  %s\n", job.ID, job.Type, string(data))
	}
}