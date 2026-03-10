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
  "priority": "<optional_priority>",
  "max_retries": "<optional_max_retries>",
  "start_at": "<optional_scheduled_time>",
  "payload": { ... }
}
```

### Parameters

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `queue` | string | Yes | The queue name (job type): `email` or `download` |
| `id` | string | No | Custom job ID (auto-generated if omitted) |
| `priority` | integer | No | Job priority - lower is higher (default: 3) |
| `max_retries` | integer | No | Maximum number of retries (default: 0) |
| `start_at` | string | No | Scheduled execution time in RFC3339 format |
| `payload` | object | Yes | Job payload data |

## Success Response

**Status Code:** `201 Created`

```json
{
  "id": "1709564234567890123",
  "type": "email",
  "queue": "queue:email",
  "priority": 3,
  "start_at": "2024-12-25T09:00:00Z",
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

## Scheduled Jobs

Jobs can be scheduled for future execution by specifying a `start_at` timestamp in RFC3339 format. The job will be held in a delayed queue until the scheduled time, then promoted to the main queue for processing.

### Create a Scheduled Email Job

```bash
# Schedule an email to be sent at a specific time
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "email",
    "start_at": "2024-12-25T09:00:00Z",
    "payload": {
      "to": "user@example.com",
      "subject": "Holiday Greeting",
      "body": "Happy Holidays!"
    }
  }'
```

### Create a Scheduled Job with Priority and Retries

```bash
# Schedule a high-priority job with retry support
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "email",
    "priority": 1,
    "max_retries": 3,
    "start_at": "2024-01-15T14:30:00Z",
    "payload": {
      "to": "important@example.com",
      "subject": "Scheduled Report",
      "body": "This is a scheduled high-priority email"
    }
  }'
```

### Create a Scheduled Report Job

```bash
# Schedule a report generation for off-peak hours
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "prepare-report",
    "start_at": "2024-01-16T02:00:00Z",
    "payload": {
      "report_type": "daily_summary",
      "start_date": "2024-01-15",
      "end_date": "2024-01-15"
    }
  }'
```

### Calculate Future Timestamp

You can use shell commands to calculate a future timestamp:

```bash
# Schedule a job for 30 minutes from now
START_AT=$(date -u -d "+30 minutes" +"%Y-%m-%dT%H:%M:%SZ")
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d "{
    \"queue\": \"email\",
    \"start_at\": \"$START_AT\",
    \"payload\": {
      \"to\": \"user@example.com\",
      \"subject\": \"Reminder\",
      \"body\": \"This is a reminder email\"
    }
  }"

# Schedule a job for tomorrow at 9 AM UTC
START_AT=$(date -u -d "tomorrow 09:00" +"%Y-%m-%dT%H:%M:%SZ")
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d "{
    \"queue\": \"email\",
    \"start_at\": \"$START_AT\",
    \"payload\": {
      \"to\": \"user@example.com\",
      \"subject\": \"Daily Report\",
      \"body\": \"Here is your daily report\"
    }
  }"
```

### How Scheduled Jobs Work

1. When a job with `start_at` is created, it's placed in the delayed queue with the timestamp as the score
2. The broker's promoter goroutine periodically checks the delayed queue
3. When `start_at` time is reached, the job is promoted to the main queue
4. Workers pick up the job from the main queue and process it normally

### Verify Scheduled Jobs

Check delayed jobs for a queue:
```bash
curl http://localhost:8080/api/delayed/email
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