# Create Job API Documentation

This document provides curl examples for creating jobs via the REST API.

## Endpoint

```
POST /api/jobs
```

## Request Format

**Headers:**
```
Content-Type: application/json
```

**Body:**
```json
{
  "queue": "<job_type>",
  "id": "<optional_custom_id>",
  "start_at": "<optional_scheduled_time>",
  "payload": { ... }
}
```

### Parameters

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `queue` | string | Yes | The queue name (job type): `email`, `download`, or `prepare-report` |
| `id` | string | No | Custom job ID (auto-generated if omitted) |
| `max_retries` | integer | No | Maximum number of retries (default: 0) |
| `priority` | integer | No | Job priority - lower is higher (default: 3) |
| `start_at` | string | No | Scheduled execution time in RFC3339 format (empty = immediate) |
| `payload` | object | Yes | Job payload data |

## Success Response

**Status Code:** `201 Created`

```json
{
  "id": "1709564234567890123",
  "type": "email",
  "queue": "queue:email",
  "priority": 3,
  "start_at": "2024-03-15T14:30:00Z",
  "payload": {
    "to": "user@example.com",
    "subject": "Welcome",
    "body": "Hello World"
  }
}
```

## curl Examples

### Create an Email Job

```bash
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "email",
    "payload": {
      "to": "user@example.com",
      "subject": "Welcome to our service",
      "body": "Thank you for signing up!"
    }
  }'
```

### Create an Email Job with Custom ID

```bash
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "email",
    "id": "welcome-email-001",
    "payload": {
      "to": "newuser@example.com",
      "subject": "Welcome!",
      "body": "Welcome to our platform"
    }
  }'
```

### Create an Email Job for Retry Testing

Emails containing "error" in the address will fail for testing retry logic:

```bash
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "email",
    "payload": {
      "to": "testerror@example.com",
      "subject": "Test Email",
      "body": "This job will fail for testing"
    }
  }'
```

### Create a Download Job

```bash
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "download",
    "payload": {
      "url": "https://example.com/files/document.pdf",
      "filename": "document.pdf"
    }
  }'
```

### Create a Download Job with Custom ID

```bash
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "download",
    "id": "download-report-2024",
    "payload": {
      "url": "https://example.com/reports/annual-2024.pdf",
      "filename": "annual-report-2024.pdf"
    }
  }'
```

### Create Multiple Jobs

```bash
for i in {1..5}; do
  curl -X POST http://localhost:8080/api/jobs \
    -H "Content-Type: application/json" \
    -d "{
      \"queue\": \"email\",
      \"payload\": {
        \"to\": \"user${i}@example.com\",
        \"subject\": \"Notification #$i\",
        \"body\": \"This is notification number $i\"
      }
    }"
done
```

## Scheduled Jobs

Jobs can be scheduled for future execution by specifying a `start_at` timestamp in RFC3339 format.

### Create a Scheduled Email Job

```bash
# Schedule an email to be sent at a specific time
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "email",
    "start_at": "2024-03-15T14:30:00Z",
    "payload": {
      "to": "scheduled@example.com",
      "subject": "Scheduled Report",
      "body": "This email was scheduled for later delivery."
    }
  }'
```

### Create a Scheduled Job with Priority and Retries

```bash
# Schedule a high-priority job with retries
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "email",
    "start_at": "2024-03-15T09:00:00Z",
    "priority": 1,
    "max_retries": 3,
    "payload": {
      "to": "important@example.com",
      "subject": "Important Scheduled Notification",
      "body": "This is a high-priority scheduled message."
    }
  }'
```

### Create a Scheduled Download Job

```bash
# Schedule a download for off-peak hours
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "download",
    "start_at": "2024-03-15T02:00:00Z",
    "payload": {
      "url": "https://example.com/large-file.zip",
      "filename": "large-file.zip"
    }
  }'
```

### Create a Scheduled Report Job

```bash
# Schedule a report generation for early morning
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "prepare-report",
    "start_at": "2024-03-16T06:00:00Z",
    "payload": {
      "report_type": "daily_summary",
      "start_date": "2024-03-15",
      "end_date": "2024-03-15"
    }
  }'
```

### Schedule Multiple Jobs at Different Times

```bash
# Schedule emails at different times
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "email",
    "start_at": "2024-03-15T09:00:00Z",
    "payload": {
      "to": "morning@example.com",
      "subject": "Morning Report",
      "body": "Good morning! Here is your daily report."
    }
  }'

curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "email",
    "start_at": "2024-03-15T12:00:00Z",
    "payload": {
      "to": "noon@example.com",
      "subject": "Lunch Reminder",
      "body": "Time for lunch!"
    }
  }'

curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "email",
    "start_at": "2024-03-15T18:00:00Z",
    "payload": {
      "to": "evening@example.com",
      "subject": "Evening Summary",
      "body": "Here is your evening summary."
    }
  }'
```

### start_at Format

The `start_at` field accepts timestamps in RFC3339 format:

| Format | Example | Description |
|--------|---------|-------------|
| UTC | `2024-03-15T14:30:00Z` | UTC timezone |
| With offset | `2024-03-15T14:30:00-05:00` | Eastern Standard Time |
| With offset | `2024-03-15T14:30:00+01:00` | Central European Time |

If `start_at` is empty or not provided, the job is enqueued immediately for processing.

## Using the Producer with Delay

The producer script supports a `-delay` flag for creating scheduled jobs without manually calculating timestamps.

### Delay Format

The `-delay` flag accepts duration strings in Go's time.Duration format:

| Format | Example | Description |
|--------|---------|-------------|
| Seconds | `30s` | 30 seconds delay |
| Minutes | `5m` | 5 minutes delay |
| Hours | `2h` | 2 hours delay |
| Combined | `1h30m` | 1 hour 30 minutes delay |
| Combined | `2h45m30s` | 2 hours 45 minutes 30 seconds delay |

### Producer Examples with Delay

```bash
# Create an email job that will execute in 5 minutes
go run ./cmd/producer -type email -count 1 -delay 5m

# Create a download job that will execute in 2 hours
go run ./cmd/producer -type download -count 1 -delay 2h

# Create a job with combined delay (1 hour 30 minutes)
go run ./cmd/producer -type email -count 1 -delay 1h30m

# Create multiple scheduled jobs with priority and retries
go run ./cmd/producer -type email -count 3 -delay 10m -priority 1 -max-retries 3

# Create a report job scheduled for 30 minutes from now
go run ./cmd/producer -type prepare-report -count 1 -delay 30m

# Create a download job scheduled for off-peak hours (e.g., 4 hours from now)
go run ./cmd/producer -type download -count 1 -delay 4h
```

### Combining Delay with Other Options

```bash
# High-priority scheduled job with retries
go run ./cmd/producer -type email -count 1 -priority 1 -max-retries 3 -delay 1h

# Multiple low-priority scheduled downloads
go run ./cmd/producer -type download -count 5 -priority 5 -delay 2h30m

# Scheduled report generation with retries
go run ./cmd/producer -type prepare-report -count 2 -max-retries 2 -delay 6h
```

## Error Responses

### 400 Bad Request - Missing Required Field

```bash
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{}'
```

Response:
```
queue is required
```

### 400 Bad Request - Invalid JSON

```bash
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d 'invalid json'
```

Response:
```
Invalid JSON: invalid character 'i' looking for beginning of value
```

### 404 Not Found - Unknown Queue

```bash
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "unknown",
    "payload": {}
  }'
```

Response:
```
queue not found: unknown
```

### 405 Method Not Allowed

```bash
curl -X GET http://localhost:8080/api/jobs
```

Response:
```
Method not allowed
```

## Verifying Created Jobs

List all queues:
```bash
curl http://localhost:8080/api/queues
```

Get pending jobs for a specific queue:
```bash
curl http://localhost:8080/api/queue/email
curl http://localhost:8080/api/queue/download
```

View in Dashboard: Open `http://localhost:8080` in your browser.