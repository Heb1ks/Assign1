#  Medical Scheduling Platform  SRE Capstone (Final)

> **Production Readiness Review**  End-term & Final Exam  
> Course: Site Reliability Engineering

---

##  Team

| Member | Step | Responsibility |
|---|---|---|
| **Altynay Ayazbayeva** | Step 1 | Infrastructure as Code (Terraform) |
| **Baldauren Zaman** | Step 2 | CI/CD Pipeline (GitHub Actions) |
| **Bakytzhan Kassymgali** | Step 3 | Observability & Alerting (Prometheus + Grafana) |
| **Eskender Rymbaev** | Step 4 | SRE Operations - SLOs, Auto-scaling & Load Testing |

---

##  Project Overview

This project deploys a production-ready **Medical Scheduling Platform** - a microservice system for managing doctors, appointments, and notifications. The platform was taken from a previous assignment and extended with full SRE practices:

- **Redis caching** (Cache-Aside, Write-Through, Write-Around) on Doctor and Appointment services
- **Background job queue** with worker pool, retry logic, idempotency, and dead-letter handling in the Notification Service
- **Redis sliding-window rate limiter** as a gRPC interceptor
- **Mock Notification Gateway** simulating an external HTTP API with 20% transient failure rate

### Architecture

```
                        ┌─────────────────────────────────────────┐
                        │              sre-network (Docker)       │
                        │                                         │
  gRPC client ---------->  doctor-service  :50051                 │
                        │       │                                 │
                        │       ▼ gRPC                            │
  gRPC client ---------->  appointment-service  :50052            │
                        │       │                                 │
                        │       │ NATS publish                    │
                        │       ▼                                 │
                        │  notification-service                   │
                        │       │ HTTP POST /notify               │
                        │       ▼                                 │
                        │  mock-gateway  :8080                    │
                        │                                         │
                        │  postgres  :5432                        │
                        │  redis     :6379                        │
                        │  nats      :4222                        │
                        └─────────────────────────────────────────┘
```

---

## Step 1 - Infrastructure as Code (Altynay Ayazbayeva)

Infrastructure is fully defined with **Terraform** using the `kreuzwerker/docker` provider. All containers are provisioned on a shared Docker network (`sre-network`) and can be reproduced from scratch with a single command.

**Provisioned resources:**

| Resource | Image | Port |
|---|---|---|
| PostgreSQL | `postgres:16-alpine` | `5432` |
| Redis | `redis:7-alpine` | `6379` |
| NATS | `nats:latest` | `4222` |

**Files:** `infrastructure/main.tf`, `infrastructure/variables.tf`, `infrastructure/outputs.tf`

### Quick Start

```bash
cd infrastructure
terraform init
terraform apply -var="db_password=postgres"
```

To tear down:

```bash
terraform destroy
```

**State** is stored locally at `terraform.tfstate`. For team collaboration the backend block in `main.tf` can be switched to a remote backend (S3, GCS, Terraform Cloud).

---

## Step 2 - CI/CD Pipeline (Baldauren Zaman)

Automated pipeline configured with **GitHub Actions** (`.github/workflows/ci.yml`). Triggers on every push to `main`.

**Pipeline steps:**

1. Checkout code
2. Login to Docker Hub (via repository secrets `DOCKER_USERNAME` / `DOCKER_PASSWORD`)
3. Build & push **doctor-service** image
4. Build & push **appointment-service** image
5. Build & push **notification-service** image
6. Build & push **mock-gateway** image

All images are tagged `:latest` and pushed to Docker Hub under `${{ secrets.DOCKER_USERNAME }}/<service-name>`.

### Required Secrets

Set these in **GitHub -> Settings -> Secrets and variables -> Actions**:

| Secret | Description |
|---|---|
| `DOCKER_USERNAME` | Docker Hub username |
| `DOCKER_PASSWORD` | Docker Hub access token |

---

## Step 3 - Observability & Alerting (Bakytzhan Kassymgali)

Monitoring stack is defined in `monitoring/docker-compose.yml` and includes **Prometheus**, **Grafana**, and **Alertmanager**.

**Files:**

| File | Purpose |
|---|---|
| `monitoring/prometheus.yml` | Scrape config (5s interval, self-monitoring) |
| `monitoring/alerts.yml` | Alert rules (`TargetDown` — critical after 10s) |
| `monitoring/alertmanager.yml` | Alertmanager routing config |

### Start Monitoring Stack

```bash
cd monitoring
docker-compose up -d
```

| Service | URL |
|---|---|
| Prometheus | http://localhost:9090 |
| Grafana | http://localhost:3000 |
| Alertmanager | http://localhost:9093 |

### Alert Rules

| Alert | Expression | Severity | Fires After |
|---|---|---|---|
| `TargetDown` | `up == 0` | critical | 10s |

To add custom SLI dashboards in Grafana: import a new dashboard and point it at the Prometheus datasource (`http://prometheus:9090`).

---

## Step 4 - SRE Operations (Eskender Rymbaev)

### SLIs & SLOs

| SLI | SLO Target | Notes |
|---|---|---|
| Availability | >= 99.5% | Services must respond to gRPC calls |
| Request success rate | >= 99% | Excluding rate-limited requests |
| p99 latency (cache hit) | < 20ms | Redis-backed reads |
| Notification delivery | >= 97% | After 3 retry attempts |

### Cache Strategy

#### Doctor Service

| Operation | Strategy | Key | TTL |
|---|---|---|---|
| `GetDoctor` | Cache-Aside | `doctor:<id>` | `CACHE_TTL_SECONDS` |
| `ListDoctors` | Cache-Aside | `doctors:list` | `CACHE_TTL_SECONDS` |
| `CreateDoctor` | Write-Through + invalidate list | `doctor:<id>`, `doctors:list` | immediate eviction for list |

#### Appointment Service

| Operation | Strategy | Key | TTL |
|---|---|---|---|
| `GetAppointment` | Cache-Aside | `appointment:<id>` | `CACHE_TTL_SECONDS` |
| `ListAppointments` | Cache-Aside | `appointments:list` | `CACHE_TTL_SECONDS` |
| `CreateAppointment` | Write-Around - invalidate list only | `appointments:list` | immediate eviction |
| `UpdateAppointmentStatus` | Write-Through + invalidate list | `appointment:<id>`, `appointments:list` | immediate eviction for list |

### Rate Limiting - Sliding Window Counter

Implemented as a gRPC `UnaryServerInterceptor` using a Redis Sorted Set (ZSET):

1. Every request adds the current nanosecond timestamp as a member in `rate_limit:<clientIP>`
2. Members older than 60 seconds are removed (`ZREMRANGEBYSCORE`)
3. `ZCARD` counts requests in the last 60s
4. If count exceeds `RATE_LIMIT_RPM` -> return `codes.ResourceExhausted`

**Why sliding window?** A fixed window allows 2x burst at minute boundaries; sliding window enforces a true per-minute limit regardless of clock boundaries.

### Auto-scaling

Custom shell-based autoscaler (`autoscaling/autoscale.sh` / `autoscaling/autoscale.ps1`) polls Docker CPU metrics and scales Docker Compose service replicas up or down.

**Tuneable parameters (env vars):**

| Variable | Default | Description |
|---|---|---|
| `SCALE_UP_THRESHOLD` | `70` | CPU % to trigger scale-up |
| `SCALE_DOWN_THRESHOLD` | `20` | CPU % to trigger scale-down |
| `SCALE_UP_CONSECUTIVE` | `2` | Consecutive polls above threshold required |
| `SCALE_DOWN_CONSECUTIVE` | `5` | Consecutive polls below threshold required |
| `COOLDOWN_SECONDS` | `30` | Pause after any scaling event |
| `MIN_REPLICAS` | `1` | Minimum replica count |
| `MAX_REPLICAS` | `5` | Maximum replica count |

Run the autoscaler alongside load testing:

```bash
# Linux/macOS
bash autoscaling/autoscale.sh

# Windows PowerShell
.\autoscaling\autoscale.ps1
```

Scaling events are logged to `autoscale.log`.

### Load Testing (Locust)

Targets the **mock-gateway** (`POST /notify`, `GET /health`) at `:8080`.

```bash
pip install locust faker

# Headless (CI / terminal)
locust -f load-testing/locustfile.py --headless -u 50 -r 5 -t 60s --host http://localhost:8080

# Web UI
locust -f load-testing/locustfile.py --host http://localhost:8080
# open http://localhost:8089
```

Apache Benchmark alternative:

```bash
# Linux/macOS
bash load-testing/ab_load_test.sh

# Windows PowerShell
.\load-testing\ab_load_test.ps1
```

### Background Job Queue

```
NATS message -> subscriber.handleMessage()
                    │
                    ▼
              pool.Submit(job)          <- buffered channel (cap=100)
                    │
         ┌──────────┼──────────┐
         ▼          ▼          ▼
      worker(0)  worker(1)  worker(2)   <- WORKER_POOL_SIZE goroutines
         │
         ▼
    processJob(job)
         ├── idempotency check  -> Redis EXISTS idempotency:<sha256>
         ├── POST /notify       -> Mock Gateway (3 attempts, exp. backoff)
         └── dead-letter        -> stderr JSON on 3rd failure
```

**Idempotency key derivation:**

```
sha256(eventType + ":" + appointmentID + ":" + timestamp_RFC3339Nano)
-> Redis key: idempotency:<hex>  TTL: 24h
```

---

##  Running the Full Stack

### Prerequisites

- Docker & Docker Compose
- Go 1.22+
- Terraform >= 1.6
- Python 3.10+ (for Locust)

### Option A - Docker Compose (recommended)

```bash
# Bring up infrastructure
docker-compose up -d

# Verify all containers are healthy
docker-compose ps
```

### Option B - Terraform

```bash
cd infrastructure
terraform init
terraform apply -var="db_password=postgres"
```

### Service Startup Order

```bash
# Terminal 1 - Mock Gateway
cd mock-gateway && go run ./cmd

# Terminal 2 - Doctor Service
cd doctor-service
DATABASE_URL="postgres://postgres:postgres@localhost:5432/doctors?sslmode=disable" \
NATS_URL="nats://localhost:4222" REDIS_URL="redis://localhost:6379" \
GRPC_ADDR=":50051" CACHE_TTL_SECONDS=60 RATE_LIMIT_RPM=100 \
go run ./cmd/doctor-service

# Terminal 3 - Appointment Service
cd appointment-service
DATABASE_URL="postgres://postgres:postgres@localhost:5432/appointments?sslmode=disable" \
NATS_URL="nats://localhost:4222" REDIS_URL="redis://localhost:6379" \
GRPC_ADDR=":50052" DOCTOR_SERVICE_ADDR="localhost:50051" \
CACHE_TTL_SECONDS=60 RATE_LIMIT_RPM=100 \
go run ./cmd/appointment-service

# Terminal 4 - Notification Service
cd notification-service
NATS_URL="nats://localhost:4222" REDIS_URL="redis://localhost:6379" \
GATEWAY_URL="http://localhost:8080" WORKER_POOL_SIZE=3 \
go run ./cmd/notification-service
```

---

##  Environment Variables

### Shared (all services)

| Variable | Default | Description |
|---|---|---|
| `REDIS_URL` | `redis://localhost:6379` | Redis connection string |
| `CACHE_TTL_SECONDS` | `60` | Cache entry TTL |

### Doctor Service

| Variable | Default |
|---|---|
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/doctors?sslmode=disable` |
| `NATS_URL` | `nats://localhost:4222` |
| `GRPC_ADDR` | `:50051` |
| `RATE_LIMIT_RPM` | `100` |

### Appointment Service

| Variable | Default |
|---|---|
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/appointments?sslmode=disable` |
| `NATS_URL` | `nats://localhost:4222` |
| `GRPC_ADDR` | `:50052` |
| `DOCTOR_SERVICE_ADDR` | `localhost:50051` |
| `RATE_LIMIT_RPM` | `100` |

### Notification Service

| Variable | Default |
|---|---|
| `NATS_URL` | `nats://localhost:4222` |
| `REDIS_URL` | `redis://localhost:6379` |
| `GATEWAY_URL` | `http://localhost:8080` |
| `WORKER_POOL_SIZE` | `3` |

### Mock Gateway

| Variable | Default |
|---|---|
| `GATEWAY_PORT` | `8080` |

---

## - Project Structure

```
.
├── .github/workflows/
│   └── ci.yml                    <- Step 2: GitHub Actions CI/CD
├── infrastructure/
│   ├── main.tf                   <- Step 1: Docker provider, containers, network
│   ├── variables.tf
│   └── outputs.tf
├── monitoring/
│   ├── docker-compose.yml        <- Step 3: Prometheus + Grafana + Alertmanager
│   ├── prometheus.yml
│   ├── alerts.yml
│   └── alertmanager.yml
├── autoscaling/
│   ├── autoscale.sh              <- Step 4: Auto-scaler (Linux/macOS)
│   └── autoscale.ps1             <-         Auto-scaler (Windows)
├── load-testing/
│   ├── locustfile.py             <- Step 4: Locust load test
│   ├── ab_load_test.sh
│   └── ab_load_test.ps1
├── doctor-service/               <- Go microservice (gRPC + Postgres + Redis)
├── appointment-service/          <- Go microservice (gRPC + Postgres + Redis + NATS)
├── notification-service/         <- Go microservice (NATS subscriber + job queue)
├── mock-gateway/                 <- Go HTTP server simulating external notification API
├── docker-compose.yml
├── docker-compose.scaling.yml
├── setup.sh
└── README.md
```

---

##  Verification Checkpoints

### Cache hit / miss

```bash
# Watch Redis in a separate terminal
docker exec -it assign1-redis-1 redis-cli MONITOR

# First call -> MISS -> DB -> Redis SET
grpcurl -plaintext -d '{"id": "<id>"}' localhost:50051 doctor.DoctorService/GetDoctor

# Second call -> HIT from Redis
grpcurl -plaintext -d '{"id": "<id>"}' localhost:50051 doctor.DoctorService/GetDoctor
```

### Rate limiter

```bash
for i in $(seq 1 110); do
  grpcurl -plaintext -d '{"id": "<id>"}' localhost:50051 doctor.DoctorService/GetDoctor 2>&1 | tail -1
done
# After 100 requests: Code: ResourceExhausted
```

### End-to-end notification flow

```bash
grpcurl -plaintext -d '{"title":"Checkup","description":"Routine","doctor_id":"<id>"}' \
  localhost:50052 appointment.AppointmentService/CreateAppointment

grpcurl -plaintext -d '{"id":"<appt_id>","status":"done"}' \
  localhost:50052 appointment.AppointmentService/UpdateAppointmentStatus
# Watch notification-service logs: enqueued → processing → success
```

### Dead-letter

Stop the mock-gateway, then trigger another `UpdateAppointmentStatus` to `done`. After 3 failed attempts a structured JSON dead-letter entry appears on stderr and the service continues normally.
