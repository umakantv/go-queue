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
  "payload": { ... }
}
```

### Parameters

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `queue` | string | Yes | The queue name (job type): `email` or `download` |
| `id` | string | No | Custom job ID (auto-generated if omitted) |
| `payload` | object | Yes | Job payload data |

## Success Response

**Status Code:** `201 Created`

```json
{
  "id": "1709564234567890123",
  "type": "email",
  "queue": "queue:email",
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