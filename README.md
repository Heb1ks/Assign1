# AP2 — Assignment 4: Caching & Background Jobs

**Student:** Eskender Rymbaev

---

## 1. Project Overview

This assignment extends the Medical Scheduling Platform from Assignment 3 with two production-readiness concerns:

1. **Redis Caching** — both Doctor Service and Appointment Service get a Redis-backed cache layer that reduces database load and improves read latency. Cache is hidden behind a `CacheRepository` interface — no Redis imports in domain or use-case code.
2. **Background Jobs & External Integration** — the Notification Service gets a worker-pool-based job queue. When an appointment transitions to `done`, a background job calls a simulated external Mock Notification Gateway over HTTP with retry logic and idempotency.

### What changed compared to Assignment 3

| Layer | Assignment 3 | Assignment 4 |
|---|---|---|
| Read path | Always hits PostgreSQL | Cache-Aside: Redis first, DB on miss |
| Write path | DB only | Write-Through / Write-Around + cache invalidation |
| Rate limiting | None | Redis sliding-window per client IP as gRPC interceptor |
| Notification Service | Logs events to stdout | Logs + background job queue + gateway calls |
| Services | Doctor + Appointment + Notification | + Mock Gateway (4th binary) |

Everything that must NOT change (domain models, use-case logic, gRPC contracts, PostgreSQL schemas, NATS integration) is identical to Assignment 3.

---

## 2. Cache Strategy

### Doctor Service

| Operation | Strategy | Redis Key | TTL |
|---|---|---|---|
| `GetDoctor` | Cache-Aside | `doctor:<id>` | `CACHE_TTL_SECONDS` |
| `ListDoctors` | Cache-Aside | `doctors:list` | `CACHE_TTL_SECONDS` |
| `CreateDoctor` | Write-Through — cache new doctor, invalidate list | `doctor:<id>`, `doctors:list` | immediate eviction for list |

### Appointment Service

| Operation | Strategy | Redis Key | TTL |
|---|---|---|---|
| `GetAppointment` | Cache-Aside | `appointment:<id>` | `CACHE_TTL_SECONDS` |
| `ListAppointments` | Cache-Aside | `appointments:list` | `CACHE_TTL_SECONDS` |
| `CreateAppointment` | Write-Around — invalidate list only | `appointments:list` | immediate eviction |
| `UpdateAppointmentStatus` | Write-Through — update cache + invalidate list | `appointment:<id>`, `appointments:list` | immediate eviction for list |

### Why Write-Around for CreateAppointment?

New appointments are rarely read immediately after creation. Write-Around avoids polluting the cache with data that may not be accessed. The individual key `appointment:<id>` is populated lazily on first `GetAppointment`.

### Why Write-Through for UpdateAppointmentStatus?

Status is a frequently-read field. Write-Through ensures the cache is immediately consistent after an update — no stale reads for the TTL window.

### Cache Invalidation Rules

- Invalidation happens after the DB write succeeds and before the gRPC response is returned.
- A cache miss never returns an error — falls through to DB transparently.
- A cache write failure is logged but does not block the gRPC response (best-effort).

### Stale-read window

Between a write and the TTL expiry, a stale value could be read if invalidation fails silently. The window is bounded by `CACHE_TTL_SECONDS` (default 60s).

---

## 3. Rate Limiting Algorithm — Sliding Window Counter

**Algorithm: Sliding Window Counter using Redis Sorted Set (ZSET)**

How it works:

1. Every request adds the current timestamp (nanoseconds) as a new member in a ZSET keyed by `rate_limit:<clientIP>`
2. `ZREMRANGEBYSCORE` removes members older than `now - 60s` (outside the window)
3. `ZCARD` counts remaining members = requests in the last 60 seconds
4. If count exceeds `RATE_LIMIT_RPM` — return `codes.ResourceExhausted`
5. `EXPIRE` resets the key TTL to 61 seconds

**Why ZSET and not INCR+TTL (fixed window)?**

A fixed window counts per minute boundary (e.g. 12:00–12:01). A client could send 100 requests at 12:00:59 and 100 more at 12:01:01 — 200 requests in 2 seconds, both windows satisfied. Sliding window counts the last 60 seconds regardless of clock boundaries.

**Implementation:** gRPC `UnaryServerInterceptor` — zero changes to handler code.

**Redis data structure:** ZSET — key `rate_limit:<clientIP>`, score = nanosecond timestamp, member = nanosecond timestamp string.

### Rate Limiting Trade-offs (per-instance vs centralised)

| Problem | Per-instance | Centralised Redis |
|---|---|---|
| Horizontal scaling | Each instance has its own counter — 3 instances x 100 RPM = 300 RPM effective limit | Single Redis counter shared by all instances — true 100 RPM globally |
| Clock skew | Each instance uses its own clock | Redis server clock is the single source of truth |

This implementation uses centralised Redis — all instances share the same ZSET key per client IP.

---

## 4. Background Job Queue Design

### Worker Pool Architecture

```
NATS message -> subscriber.handleMessage()
                    |
                    v
              pool.Submit(job)
                    |
                    v
         jobsChan (buffered, cap=100)
                    |
         +----------+-----------+
         v          v           v
      worker(0)  worker(1)  worker(2)   <- goroutines, count = WORKER_POOL_SIZE
         |
         v
    processJob(job)
         |
    +----+---------------------+
    |  idempotency check       | -> Redis EXISTS
    |  POST /notify            | -> Mock Gateway
    |  retry + backoff         |
    |  dead-letter             | -> stderr
    +--------------------------+
```

- **Channel capacity:** 100 buffered slots. If all workers are busy and the buffer is full, `Submit()` uses a `select` with `ctx.Done()` to avoid blocking forever.
- **Backpressure:** If channel is full, the job is dropped with a warning log. In production this would trigger an alert.
- **Graceful shutdown:** `Stop()` calls `cancel()` (signals workers via context), then `wg.Wait()` (waits for all goroutines to finish), then `close(jobsChan)`.

### Job Lifecycle

```
1. Event received from NATS (appointments.status_updated, new_status=done)
2. Job created and submitted to channel
3. Worker picks up job
4. Idempotency check: Redis EXISTS idempotency:<sha256>
   -> exists: log "duplicate dropped", return
5. Log: status="enqueued"
6. Attempt 1: POST /notify
   -> 200: Redis SET idempotency key (TTL 24h), log status="success"
   -> 503: log status="retry", sleep 1s, attempt 2
7. Attempt 2: POST /notify
   -> 200: success
   -> 503: log status="retry", sleep 2s, attempt 3
8. Attempt 3: POST /notify
   -> 200: success
   -> 503: write dead_letter to stderr, drop job
```

---

## 5. Idempotency

**Key derivation:**

```go
input := fmt.Sprintf("%s:%s:%s", eventType, id, timestamp.UTC().Format(time.RFC3339Nano))
hash  := sha256.Sum256([]byte(input))
key   := fmt.Sprintf("idempotency:%x", hash[:])
```

**Storage:** Redis string, value `"done"`, TTL 24 hours (86400 seconds).

**Why SHA-256?** The key must be deterministic (same event produces same key) and compact. SHA-256 of `eventType + appointmentID + timestamp` is unique per event occurrence.

**Why 24h TTL?** Long enough to survive a service restart and prevent reprocessing of recently delivered events. After 24h the key expires and a theoretically replayed event would be reprocessed — acceptable for notifications.

**How it prevents duplicates on retry:** Before calling the gateway, the worker checks `EXISTS idempotency:<key>`. If found, the job is silently dropped. If the worker crashes mid-flight and the key was not yet set, the job will be reprocessed on restart — that is intentional (at-least-once delivery).

---

## 6. Dead-Letter Strategy

After 3 failed attempts, the worker writes a structured JSON entry to stderr:

```json
{
  "time": "2026-05-01T10:25:10Z",
  "level": "error",
  "job_id": "idempotency:abc123...",
  "attempt": 3,
  "status": "dead_letter",
  "error": "service unavailable"
}
```

The worker does not crash — it continues processing the next job from the channel.

**How to inspect dead-letter entries:**

```bash
# Redirect stderr to a file when starting the service
go run ./cmd/notification-service 2> dead_letters.log

# Then inspect
cat dead_letters.log
```

**What a production system would do:**

- Publish dead-letter events to a dedicated broker subject (e.g. `appointments.notifications.dead_letter`) so ops teams can replay them
- Set up alerting (PagerDuty, Grafana) when dead-letter count exceeds a threshold
- Store dead-letter payloads in a database table for manual inspection and replay

---

## 7. Mock Notification Gateway

A minimal fourth Go binary that simulates an idempotent external notification API.

**Endpoint:** `POST /notify`

```json
// Request
{ "idempotency_key": "...", "channel": "email", "recipient": "patient@clinic.kz", "message": "..." }

// Response — new key
{ "status": "accepted" }

// Response — duplicate key
{ "status": "duplicate" }

// Response — 20% of the time (transient failure simulation)
// HTTP 503
{ "error": "service unavailable" }
```

**Idempotency:** tracked in-memory via `map[string]struct{}` + `sync.Mutex`. Restarting the gateway clears the map (intentional for the demo — Redis-backed in production).

**20% failure rate:** `rand.Intn(100) < 20` — exercises retry logic during defense.

---

## 8. Environment Variables

### All Services

| Variable | Default | Description |
|---|---|---|
| `REDIS_URL` | `redis://localhost:6379` | Redis connection string |
| `CACHE_TTL_SECONDS` | `60` | Cache entry TTL in seconds |

### Doctor Service

| Variable | Default | Description |
|---|---|---|
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/doctors?sslmode=disable` | PostgreSQL DSN |
| `NATS_URL` | `nats://localhost:4222` | NATS server URL |
| `GRPC_ADDR` | `:50051` | gRPC listen address |
| `RATE_LIMIT_RPM` | `100` | Max requests per minute per client IP |

### Appointment Service

| Variable | Default | Description |
|---|---|---|
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/appointments?sslmode=disable` | PostgreSQL DSN |
| `NATS_URL` | `nats://localhost:4222` | NATS server URL |
| `GRPC_ADDR` | `:50052` | gRPC listen address |
| `DOCTOR_SERVICE_ADDR` | `localhost:50051` | Doctor Service gRPC address |
| `RATE_LIMIT_RPM` | `100` | Max requests per minute per client IP |

### Notification Service

| Variable | Default | Description |
|---|---|---|
| `NATS_URL` | `nats://localhost:4222` | NATS server URL |
| `REDIS_URL` | `redis://localhost:6379` | Redis (idempotency store) |
| `GATEWAY_URL` | `http://localhost:8080` | Mock Gateway URL |
| `WORKER_POOL_SIZE` | `3` | Number of background job workers |

### Mock Gateway

| Variable | Default | Description |
|---|---|---|
| `GATEWAY_PORT` | `8080` | HTTP listen port |

---

## 9. Infrastructure Setup

### Docker Compose (recommended)

```bash
# Start PostgreSQL, Redis, NATS all at once
docker-compose up -d

# Check all are healthy
docker-compose ps
```

### Manual Docker commands

```bash
# PostgreSQL
docker run -d --name pg -e POSTGRES_PASSWORD=postgres -p 5432:5432 postgres:16-alpine
docker exec -it pg psql -U postgres -c "CREATE DATABASE doctors;"
docker exec -it pg psql -U postgres -c "CREATE DATABASE appointments;"

# Redis
docker run -d --name redis -p 6379:6379 redis:7-alpine

# NATS
docker run -d --name nats -p 4222:4222 nats:latest
```

---

## 10. Service Startup Order

Start in this exact order (each in its own terminal):

```bash
# Terminal 1 — Mock Gateway (start first so Notification Service can reach it)
cd mock-gateway
GATEWAY_PORT=8080 go run ./cmd

# Terminal 2 — Doctor Service
cd doctor-service
DATABASE_URL="postgres://postgres:postgres@localhost:5432/doctors?sslmode=disable" \
NATS_URL="nats://localhost:4222" \
REDIS_URL="redis://localhost:6379" \
GRPC_ADDR=":50051" \
CACHE_TTL_SECONDS=60 \
RATE_LIMIT_RPM=100 \
go run ./cmd/doctor-service

# Terminal 3 — Appointment Service
cd appointment-service
DATABASE_URL="postgres://postgres:postgres@localhost:5432/appointments?sslmode=disable" \
NATS_URL="nats://localhost:4222" \
REDIS_URL="redis://localhost:6379" \
GRPC_ADDR=":50052" \
DOCTOR_SERVICE_ADDR="localhost:50051" \
CACHE_TTL_SECONDS=60 \
RATE_LIMIT_RPM=100 \
go run ./cmd/appointment-service

# Terminal 4 — Notification Service
cd notification-service
NATS_URL="nats://localhost:4222" \
REDIS_URL="redis://localhost:6379" \
GATEWAY_URL="http://localhost:8080" \
WORKER_POOL_SIZE=3 \
go run ./cmd/notification-service
```

**Windows PowerShell** — set env vars separately:

```powershell
$env:DATABASE_URL="postgres://postgres:postgres@localhost:5432/doctors?sslmode=disable"
$env:NATS_URL="nats://localhost:4222"
$env:REDIS_URL="redis://localhost:6379"
$env:GRPC_ADDR=":50051"
$env:CACHE_TTL_SECONDS="60"
$env:RATE_LIMIT_RPM="100"
go run ./cmd/doctor-service
```

---

## 11. Cache Consistency Trade-offs

### Redis unavailable

If Redis is unreachable at startup, the service logs a warning and continues with a no-op cache (`client = nil`). All cache methods return immediately without error. The service serves all requests from PostgreSQL — caching is a pure performance optimisation, never a single point of failure.

### Which reads become eventually consistent

After a write, the list cache (`doctors:list`, `appointments:list`) is immediately invalidated. Individual entity keys (`doctor:<id>`, `appointment:<id>`) are updated via Write-Through on the same request. There is no eventual consistency window for entities — only a bounded staleness window equal to `CACHE_TTL_SECONDS` if invalidation silently fails.

### Distributed cache (Redis Cluster) trade-offs

| Concern | Single Redis | Redis Cluster |
|---|---|---|
| Key invalidation | Atomic DEL | Key must hash to the same slot; cross-slot transactions require hash tags |
| Consistency | Strong (single node) | Strong within a slot; cross-slot operations are not atomic |
| Availability | Single point of failure | Automatic failover via cluster bus |

---

## 12. grpcurl Commands and Expected Output

### Checkpoint 1 — Cache Hit

```bash
# Open Redis MONITOR in a separate terminal first
docker exec -it assign1-redis-1 redis-cli MONITOR

# Create a doctor
grpcurl -plaintext -d '{
  "full_name": "Dr. Aisha Seitkali",
  "specialization": "Cardiology",
  "email": "a.seitkali@clinic.kz"
}' localhost:50051 doctor.DoctorService/CreateDoctor

# First call — MISS -> DB -> SET in Redis
grpcurl -plaintext -d '{"id": "<id>"}' localhost:50051 doctor.DoctorService/GetDoctor

# Second call — HIT from Redis, no DB query
grpcurl -plaintext -d '{"id": "<id>"}' localhost:50051 doctor.DoctorService/GetDoctor
```

**Doctor Service terminal:**

```
[DEBUG] Cache SET: doctor:<id> (TTL=60s)   <- first call
[DEBUG] Cache HIT: doctor:<id>             <- second call
```

---

### Checkpoint 2 — Rate Limiter

```bash
# Send 110 requests — after 100 you will get ResourceExhausted
for i in $(seq 1 110); do
  grpcurl -plaintext -d '{"id": "<id>"}' localhost:50051 doctor.DoctorService/GetDoctor 2>&1 | tail -1
done
```

**Expected error after limit:**

```
Code: ResourceExhausted
Message: rate limit exceeded: 100 requests per minute allowed; retry after 60 seconds
```

---

### Checkpoint 3 — Job Queue and Gateway

```bash
# Create doctor
grpcurl -plaintext -d '{
  "full_name": "Dr. Smith",
  "specialization": "Neurology",
  "email": "smith@clinic.kz"
}' localhost:50051 doctor.DoctorService/CreateDoctor

# Create appointment
grpcurl -plaintext -d '{
  "title": "Checkup",
  "description": "Routine checkup",
  "doctor_id": "<doctor_id>"
}' localhost:50052 appointment.AppointmentService/CreateAppointment

# Trigger job — update status to done
grpcurl -plaintext -d '{
  "id": "<appointment_id>",
  "status": "done"
}' localhost:50052 appointment.AppointmentService/UpdateAppointmentStatus
```

**Notification Service terminal:**

```json
{"time":"...","level":"info","msg":"event received","data":{"subject":"appointments.status_updated"}}
{"time":"...","level":"info","job_id":"idempotency:abc...","status":"enqueued"}
{"time":"...","level":"info","job_id":"idempotency:abc...","attempt":1,"status":"processing"}
{"time":"...","level":"info","job_id":"idempotency:abc...","attempt":1,"status":"success"}
```

**Mock Gateway terminal:**

```json
{"time":"...","result":"accepted","idempotency_key":"idempotency:abc...","status_code":200}
```

---

### Checkpoint 4 — Idempotency

Replay the same event manually via NATS CLI after a successful Checkpoint 3:

```bash
nats pub appointments.status_updated '{
  "event_type":"appointments.status_updated",
  "occurred_at":"<SAME_TIMESTAMP>",
  "id":"<SAME_APPOINTMENT_ID>",
  "old_status":"scheduled",
  "new_status":"done",
  "doctor_id":"<SAME_DOCTOR_ID>"
}'
```

**Notification Service terminal:**

```json
{"time":"...","level":"info","msg":"event received"}
{"time":"...","level":"info","msg":"duplicate job dropped (already processed)","data":{"job_id":"idempotency:abc..."}}
```

No second POST to the gateway.

---

### Checkpoint 5 — Dead Letter

```bash
# Stop the Mock Gateway (Ctrl+C in its terminal)
# Then trigger another UpdateAppointmentStatus to done (new appointment)
```

**Notification Service terminal:**

```json
{"time":"...","level":"info","job_id":"...","attempt":1,"status":"processing"}
{"time":"...","level":"warn","job_id":"...","attempt":1,"status":"retry","error":"service unavailable"}
{"time":"...","level":"info","job_id":"...","attempt":2,"status":"processing"}
{"time":"...","level":"warn","job_id":"...","attempt":2,"status":"retry","error":"service unavailable"}
{"time":"...","level":"info","job_id":"...","attempt":3,"status":"processing"}
{"time":"...","level":"warn","job_id":"...","attempt":3,"status":"retry","error":"service unavailable"}
```

**stderr:**

```json
{"time":"...","level":"error","job_id":"...","attempt":3,"status":"dead_letter","error":"service unavailable"}
```

Service continues running normally after dead-letter.

---

## 13. Event Contract

| Subject | Publisher | Trigger | JSON Fields |
|---|---|---|---|
| `doctors.created` | Doctor Service | `CreateDoctor` succeeds | `event_type`, `occurred_at`, `id`, `full_name`, `specialization`, `email` |
| `appointments.created` | Appointment Service | `CreateAppointment` succeeds | `event_type`, `occurred_at`, `id`, `title`, `doctor_id`, `status` |
| `appointments.status_updated` | Appointment Service | `UpdateAppointmentStatus` succeeds | `event_type`, `occurred_at`, `id`, `old_status`, `new_status`, `doctor_id` |

Job queue is triggered only by `appointments.status_updated` where `new_status = "done"`.

---

## 14. Project Structure

```
ap2-assignment4/
├── doctor-service/
│   ├── cmd/doctor-service/main.go
│   ├── internal/
│   │   ├── model/doctor.go
│   │   ├── repository/
│   │   │   ├── doctor_repository.go
│   │   │   └── postgres_doctor_repository.go
│   │   ├── usecase/doctor_usecase.go        <- Cache-Aside reads, Write-Through writes
│   │   ├── cache/
│   │   │   ├── repository.go               <- CacheRepository interface
│   │   │   └── redis_cache.go              <- Redis implementation
│   │   ├── middleware/
│   │   │   └── rate_limiter.go             <- gRPC UnaryServerInterceptor
│   │   ├── event/publisher.go
│   │   ├── transport/grpc/handler.go
│   │   └── app/app.go
│   ├── migrations/
│   │   ├── 000001_create_doctors.up.sql
│   │   └── 000001_create_doctors.down.sql
│   ├── proto/
│   └── go.mod
├── appointment-service/
│   ├── cmd/appointment-service/main.go
│   ├── internal/
│   │   ├── model/appointment.go
│   │   ├── repository/
│   │   ├── usecase/appointment_usecase.go   <- Write-Around create, Write-Through update
│   │   ├── cache/
│   │   │   ├── repository.go
│   │   │   └── redis_cache.go
│   │   ├── middleware/
│   │   │   └── rate_limiter.go
│   │   ├── event/publisher.go
│   │   ├── client/
│   │   ├── transport/grpc/handler.go
│   │   └── app/app.go
│   ├── migrations/
│   ├── proto/
│   └── go.mod
├── notification-service/
│   ├── cmd/notification-service/main.go
│   ├── internal/
│   │   └── subscriber/
│   │       ├── subscriber.go               <- NATS subscriber
│   │       ├── logger/logger.go            <- structured JSON logger
│   │       ├── jobqueue/
│   │       │   ├── job.go                  <- Job struct + Notifier interface
│   │       │   ├── worker_pool.go          <- goroutine pool + retry + dead-letter
│   │       │   └── idempotency.go          <- SHA-256 key + CacheRepository interface
│   │       └── gateway/gateway.go          <- HTTP client for Mock Gateway
│   └── go.mod
├── mock-gateway/
│   ├── cmd/main.go                         <- POST /notify, 20% 503, idempotency map
│   └── go.mod
├── docker-compose.yml
└── README.md
```
