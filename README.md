# go-queue

A Redis-based Golang package to manage a queue system, supporting multiple configured job types with dedicated queues for segregation.

## Features

- **Multiple Job Types**: Support for different job types with dedicated Redis queues
- **Job Priorities**: Jobs can be assigned priority levels (1, 2, 3...), where lower numbers indicate higher priority (default: 3)
- **Built-in Concurrency**: Workers use goroutines for parallel job processing
- **Real-time Dashboard**: SSE-enabled dashboard displaying queue stats and pending tasks
- **Non-Blocking Retries**: Failed jobs are re-queued to a delayed queue using Redis sorted sets, allowing workers to remain available
- **Dead-Letter Queue**: Jobs that exceed max retries are moved to a dead-letter queue with full error history
- **Job Replay**: Failed jobs can be replayed from the dashboard UI
- **REST API**: HTTP endpoints for job management and monitoring

## Installation

```bash
go get github.com/umakantv/redis-queue
```

## Quick Start

### Prerequisites

- Go 1.21 or higher
- Redis server running locally or accessible via network

### Running the Components

1. **Start Redis** (if not already running):
   ```bash
   redis-server
   ```

2. **Start the Dashboard**:
   ```bash
   go run ./cmd/dashboard
   ```
   The dashboard will be available at `http://localhost:8080`

3. **Start the Broker** (separate terminal):
   ```bash
   # Handles background job promotions for all types
   go run ./cmd/broker
   ```

4. **Start Workers** (in separate terminals):
   ```bash
   # Email worker
   go run ./cmd/email -concurrency 2
   
   # Download worker
   go run ./cmd/download
   
   # Prepare-report worker (long-running jobs)
   go run ./cmd/prepare-report -concurrency 2
   ```

5. **Produce Jobs**:
   ```bash
   # Create email jobs
   go run ./cmd/producer -type email -count 5
   
   # Create download jobs
   go run ./cmd/producer -type download -count 3
   
   # Create prepare-report jobs (long-running)
   go run ./cmd/producer -type prepare-report -count 2
   
   # Create high-priority jobs (priority 1 = highest)
   go run ./cmd/producer -type email -count 3 -priority 1
   
   # Create low-priority jobs (priority 5 = lower than default)
   go run ./cmd/producer -type download -count 5 -priority 5
   
   # List pending jobs
   go run ./cmd/producer -list
   ```

## Worker Configuration

Workers support the following command-line flags:

| Flag | Default | Description |
|------|---------|-------------|
| `-concurrency` | 1 | Number of concurrent worker goroutines |
| `-poll-interval` | 100ms | Interval between polling attempts |
| `-retry-delay` | 1s | Delay before retrying a failed job |


Example:
```bash
go run ./cmd/email -concurrency 4 -retry-delay 2s
```

## Job Priorities

Jobs can be assigned a priority level using positive integers. Lower numbers indicate higher priority.

- **Priority 1**: Highest priority (processed first)
- **Priority 2**: High priority
- **Priority 3**: Normal priority (default)
- **Priority 4+**: Lower priority (processed after higher priority jobs)

### Setting Priority via API

When creating a job via the REST API, include the `priority` field:

```bash
# Create a high-priority email job (priority 1)
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "email",
    "priority": 1,
    "payload": {
      "to": "urgent@example.com",
      "subject": "Urgent: Action Required",
      "body": "This is a high-priority message!"
    }
  }'
```

### Setting Priority via Producer

Use the `-priority` flag when running the producer:

```bash
# Create 5 high-priority jobs
go run ./cmd/producer -type email -count 5 -priority 1

# Create 3 low-priority jobs
go run ./cmd/producer -type download -count 3 -priority 5
```

### Priority in Job Structure

When creating jobs programmatically:

```go
import redisqueue "github.com/umakantv/redis-queue/redisqueue"

// Create a high-priority job
job, err := registry.Enqueue(ctx, "email", payload, 3, 1) // maxRetries=3, priority=1

// Create a job with default priority
job, err := registry.Enqueue(ctx, "email", payload, 3, 0) // 0 uses default (3)
```

## Queue Architecture

### Redis Keys

| Purpose | Key Pattern | Type |
|---------|-------------|------|
| Main queue | `queue:<type>` | List |
| Delayed retry queue | `queue:<type>:delayed` | Sorted Set |
| Dead-letter queue | `queue:<type>:dead` | List |

### Job Lifecycle

```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│   Enqueue   │───▶│    Main     │───▶│   Worker    │
│   (API)     │    │   Queue     │    │  Processing │
└─────────────┘    └─────────────┘    └──────┬──────┘
                                             │
                                   ┌─────────▼─────────┐
                                   │  Handler Success? │
                                   └─────────┬─────────┘
                                             │
                            ┌────────────────┼─────────────────┐
                            │                │                 │
                     ┌──────▼──────┐  ┌──────▼───────┐  ┌──────▼───────┐
                     │   Success   │  │   Failed     │  │   Failed     │
                     │  (done)     │  │ (retry < max)│  │ (retry >=max)│
                     └─────────────┘  └──────┬───────┘  └──────┬───────┘
                                             │                 │
                                      ┌──────▼──────┐  ┌───────▼─────┐
                                      │   Delayed   │  │  Dead-Letter│
                                      │   Queue     │  │   Queue     │
                                      └──────┬──────┘  └─────────────┘
                                             │
                                     ┌───────▼───────┐
                                     │  Promoter     │
                                     │ (after delay) │
                                     └───────┬───────┘
                                             │
                                      ┌──────▼──────┐
                                      │    Main     │
                                      │   Queue     │
                                      └─────────────┘
```

### Non-Blocking Retry Mechanism

When a job fails and has `max_retries` > 0:

1. The job is **immediately** re-enqueued to the delayed queue (sorted set) with a timestamp score
2. The worker **does not block** - it's immediately available to process other jobs
3. A separate promoter goroutine moves ready jobs back to the main queue when the delay expires
4. This ensures **full worker utilization** even during retry periods

**Retry Behavior:**
- Jobs with `max_retries: 0` (default) will not retry and go directly to the dead-letter queue on failure
- Jobs with `max_retries: N` will be retried up to N times before moving to dead-letter queue
- The retry delay is controlled by the worker's `-retry-delay` flag (default: 1s)

### Dead-Letter Queue

Jobs that fail after reaching the maximum retry count are moved to the dead-letter queue with:
- Full error history for each attempt
- Timestamps of each failure
- Original job payload

## API Endpoints

### Dashboard API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/` | GET | Web dashboard UI |
| `/api/queues` | GET | List all queues with stats (pending, delayed, dead) |
| `/api/queue/{type}` | GET | Get pending jobs for a specific queue |
| `/api/jobs` | POST | Create a new job |
| `/api/delayed/{type}` | GET | List delayed jobs with execution times |
| `/api/dead-letter/{type}` | GET | List dead-letter jobs with error history |
| `/api/dead-letter/{type}` | DELETE | Clear all dead-letter jobs |
| `/api/replay-job/{type}/{id}` | POST | Replay a dead-letter job |
| `/events` | GET | SSE stream for real-time updates |

### Create Job API

**POST /api/jobs**

Create a new job and insert it into the specified queue.

**Request Body:**
```json
{
  "queue": "email",
  "id": "optional-custom-id",
  "payload": {
    "to": "user@example.com",
    "subject": "Hello",
    "body": "Hello World"
  }
}
```

**Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `queue` | string | Yes | Queue name (job type): `email`, `download`, or `prepare-report` |
| `id` | string | No | Custom job ID (auto-generated if omitted) |
| `max_retries` | integer | No | Maximum number of retries (default: 0) |
| `priority` | integer | No | Job priority - lower is higher (default: 3) |
| `payload` | object | Yes | Job payload data |


**Response (201 Created):**
```json
{
  "id": "1709564234567890123",
  "type": "email",
  "queue": "queue:email",
  "priority": 3,
  "payload": {
    "to": "user@example.com",
    "subject": "Hello",
    "body": "Hello World"
  }
}
```

**Example Usage:**
```bash
# Create an email job
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "email",
    "payload": {
      "to": "user@example.com",
      "subject": "Welcome",
      "body": "Welcome to our service!"
    }
  }'

# Create a high-priority email job
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "email",
    "priority": 1,
    "max_retries": 3,
    "payload": {
      "to": "urgent@example.com",
      "subject": "Urgent",
      "body": "High priority message!"
    }
  }'

# Create a download job
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "download",
    "payload": {
      "url": "https://example.com/file.pdf",
      "filename": "document.pdf"
    }
  }'
```

See [docs/create-job.md](docs/create-job.md) for more examples.

### Replay Job API

**POST /api/replay-job/{type}/{id}**

Replay a failed job from the dead-letter queue. The job is reset (retry count cleared) and re-queued for processing.

```bash
curl -X POST http://localhost:8080/api/replay-job/email/abc123
```

## Dashboard UI Features

The web dashboard provides:

- **Queue Cards**: Shows pending, delayed, and dead-letter counts for each queue
- **Tabbed Job Views**:
  - **Pending**: Active jobs waiting to be processed
  - **Delayed**: Jobs waiting for retry with execution times
  - **Dead Letter**: Failed jobs with expandable error history
- **Actions**:
  - Clear all dead-letter jobs
  - Replay individual dead-letter jobs
- **Real-time Updates**: SSE-powered live updates without page refresh

## Job Types

### Email Jobs

Queue: `email` (Redis key: `queue:email`)

Payload structure:
```json
{
  "to": "recipient@example.com",
  "subject": "Email Subject",
  "body": "Email body content"
}
```

### Download Jobs

Queue: `download` (Redis key: `queue:download`)

Payload structure:
```json
{
  "url": "https://example.com/file.pdf",
  "filename": "document.pdf"
}
```

### Prepare-Report Jobs

Queue: `prepare-report` (Redis key: `queue:prepare-report`)

Payload structure:
```json
{
  "report_type": "monthly_sales",
  "start_date": "2024-01-01",
  "end_date": "2024-01-31"
}
```

**Characteristics:**
- **Processing Time**: 20-40 seconds per job (simulates long-running report generation)
- **Use Case**: Demonstrates how the queue system handles jobs with indefinite or long processing times
- **Progress Tracking**: Logs progress updates every 5 seconds during report generation
- **Context Awareness**: Respects cancellation signals for graceful shutdown

**Example Usage:**
```bash
# Create a prepare-report job via API
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "prepare-report",
    "payload": {
      "report_type": "quarterly_financial",
      "start_date": "2024-01-01",
      "end_date": "2024-03-31"
    }
  }'

# Start the prepare-report worker
go run ./cmd/prepare-report -concurrency 2
```

## Configuration

Environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `REDIS_ADDR` | `localhost:6379` | Redis server address |
| `REDIS_PASSWORD` | `""` | Redis password |
| `PORT` | `8080` | Dashboard port |
| `DASHBOARD_URL` | `http://localhost:8080` | Dashboard URL (used by producer) |

## Testing Retry Logic

The producer script simulates job failures for email jobs containing "error" in the email address. This allows testing of retry logic:

```bash
# Create an email job that will fail and be retried 2 times
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "email",
    "max_retries": 2,
    "payload": {
      "to": "usererror@example.com",
      "subject": "Test Retry",
      "body": "This job will fail for testing"
    }
  }'
```

Or using the producer command:

```bash
# Create 5 email jobs with 2 retries each
# Jobs with "error" in the email address will fail and be retried
go run ./cmd/producer -type email -count 5 -max-retries 2
```

## License

MIT License