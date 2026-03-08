package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"
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

// CreateJobRequest represents the request body for creating a job.
type CreateJobRequest struct {
	Queue   string      `json:"queue"`
	ID      string      `json:"id,omitempty"`
	Payload interface{} `json:"payload"`
}

// CreateJobResponse represents the response from creating a job.
type CreateJobResponse struct {
	ID      string                 `json:"id"`
	Type    string                 `json:"type"`
	Queue   string                 `json:"queue"`
	Payload map[string]interface{} `json:"payload"`
}

// QueueStats represents statistics for a queue.
type QueueStats struct {
	Name      string `json:"name"`
	JobType   string `json:"job_type"`
	Size      int64  `json:"size"`
	QueueName string `json:"queue_name"`
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
	serverURL := flag.String("server", getEnv("DASHBOARD_URL", "http://localhost:8080"), "Dashboard server URL")
	flag.Parse()

	// List mode
	if *list {
		listQueues(*serverURL)
		return
	}

	// Validate job type
	if *jobType == "" {
		fmt.Println("Error: -type flag is required")
		fmt.Println("Usage:")
		fmt.Println("  producer -type email [-count N]    Create N email jobs")
		fmt.Println("  producer -type download [-count N] Create N download jobs")
		fmt.Println("  producer -list                     List pending jobs in queues")
		fmt.Println("  producer -server URL               Dashboard server URL (default: http://localhost:8080)")
		os.Exit(1)
	}

	// Seed random
	rand.Seed(time.Now().UnixNano())

	// Create jobs
	var created int
	switch *jobType {
	case "email":
		for i := 0; i < *count; i++ {
			payload := generateEmailPayload()
			job, err := createJob(*serverURL, "email", payload)
			if err != nil {
				log.Printf("Failed to create email job: %v", err)
				continue
			}
			fmt.Printf("Created email job %s: to=%s, subject=%s\n", job.ID, payload.To, payload.Subject)
			created++
		}
		fmt.Printf("\nCreated %d email job(s)\n", created)

	case "download":
		for i := 0; i < *count; i++ {
			payload := generateDownloadPayload()
			job, err := createJob(*serverURL, "download", payload)
			if err != nil {
				log.Printf("Failed to create download job: %v", err)
				continue
			}
			fmt.Printf("Created download job %s: url=%s, filename=%s\n", job.ID, payload.URL, payload.Filename)
			created++
		}
		fmt.Printf("\nCreated %d download job(s)\n", created)

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

// createJob sends a request to the dashboard API to create a job.
func createJob(serverURL, queue string, payload interface{}) (*CreateJobResponse, error) {
	req := CreateJobRequest{
		Queue:   queue,
		Payload: payload,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/api/jobs", serverURL)
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var job CreateJobResponse
	if err := json.Unmarshal(respBody, &job); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &job, nil
}

// listQueues fetches and displays queue status from the dashboard API.
func listQueues(serverURL string) {
	url := fmt.Sprintf("%s/api/queues", serverURL)
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		log.Fatalf("Failed to fetch queue status: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Failed to read response: %v", err)
	}

	var queues []QueueStats
	if err := json.Unmarshal(body, &queues); err != nil {
		log.Fatalf("Failed to parse response: %v", err)
	}

	fmt.Println("Queue Status:")
	fmt.Println("=============")
	for _, q := range queues {
		fmt.Printf("  %s queue: %d pending jobs\n", q.Name, q.Size)
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