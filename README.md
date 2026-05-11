# AP2 — Assignment 3: Message Queue & Database Migrations

**Student:** Eskender Rymabev

---

## 1. Project Overview

This assignment extends the Medical Scheduling Platform built in Assignment 2 in two directions:

1. **PostgreSQL persistence** — both existing services replace their in-memory maps with a real PostgreSQL database. Schema is managed exclusively through `golang-migrate` versioned migration files.
2. **Asynchronous event-driven communication via NATS** — every successful write operation publishes a domain event. A new third service, the **Notification Service**, subscribes to all events and prints a structured JSON log line to stdout.

### What changed compared to Assignment 2

| Layer | Assignment 2 | Assignment 3 |
|---|---|---|
| Repository | In-memory maps | PostgreSQL via `database/sql` + `pgx/v5` |
| Schema management | None | `golang-migrate` migration files |
| Inter-service async | None | NATS Core Pub/Sub |
| Services | Doctor + Appointment | Doctor + Appointment + **Notification** |

Everything that must NOT change (domain models, use-case logic, gRPC contracts, Clean Architecture layering) is identical to Assignment 2.

---

## 2. Broker Choice — NATS (Core)

**Chosen broker: NATS (Core)**

**Reason:** NATS Core perfectly fits our use case of stateless, fire-and-forget notifications. It requires zero configuration beyond starting the server binary (or a single Docker container), has no message persistence overhead, and its Go client (`nats.go`) is idiomatic and lightweight. Since the Notification Service only needs to log events and does not need guaranteed delivery or replay, the simplicity of NATS Core is the right trade-off here.

### NATS vs RabbitMQ — two concrete differences

| | NATS (Core) | RabbitMQ |
|---|---|---|
| **Persistence** | None — messages are fire-and-forget; if no subscriber is connected at publish time, the message is lost | Queue-level durability; messages survive broker restart and are held until a consumer acknowledges them |
| **Delivery model** | Pure Pub/Sub; every active subscriber on a subject receives every message | Flexible routing via exchanges (fanout, topic, direct); point-to-point queues give each message to exactly one consumer |

**When to choose RabbitMQ:** when guaranteed at-least-once delivery is required (e.g., sending an email confirmation, billing events) or when messages must survive broker restarts. NATS JetStream is the equivalent durable option within the NATS ecosystem.

---

## 3. Architecture Diagram

```
┌──────────────────────────────────────────────────────────────────┐
│                    Medical Scheduling Platform                    │
│                                                                  │
│  ┌─────────────────────┐     gRPC      ┌──────────────────────┐ │
│  │   Doctor Service    │◄──────────────│ Appointment Service  │ │
│  │   :50051            │               │ :50052               │ │
│  │                     │               │                      │ │
│  │  ┌───────────────┐  │               │  ┌────────────────┐  │ │
│  │  │  PostgreSQL   │  │               │  │  PostgreSQL    │  │ │
│  │  │  (doctors DB) │  │               │  │ (appointments) │  │ │
│  │  └───────────────┘  │               │  └────────────────┘  │ │
│  │                     │               │                      │ │
│  │  Publishes:         │               │  Publishes:          │ │
│  │  doctors.created ───┼──────────┐    │  appointments.───────┼─┤
│  └─────────────────────┘          │    │  created             │ │
│                                   │    │  appointments.───────┼─┤
│                                   │    │  status_updated      │ │
│                                   ▼    └──────────────────────┘ │
│                           ┌───────────────┐         │           │
│                           │  NATS Server  │◄────────┘           │
│                           │  :4222        │                     │
│                           └───────┬───────┘                     │
│                                   │                             │
│                                   ▼                             │
│                    ┌──────────────────────────┐                 │
│                    │   Notification Service   │                 │
│                    │   (subscriber, no port)  │                 │
│                    │                          │                 │
│                    │  Logs JSON to stdout     │                 │
│                    └──────────────────────────┘                 │
└──────────────────────────────────────────────────────────────────┘
```

---

## 4. Environment Variables

### Doctor Service

| Variable | Default | Description |
|---|---|---|
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/doctors?sslmode=disable` | PostgreSQL DSN |
| `NATS_URL` | `nats://localhost:4222` | NATS server URL |
| `GRPC_ADDR` | `:50051` | gRPC listen address |

### Appointment Service

| Variable | Default | Description |
|---|---|---|
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/appointments?sslmode=disable` | PostgreSQL DSN |
| `NATS_URL` | `nats://localhost:4222` | NATS server URL |
| `GRPC_ADDR` | `:50052` | gRPC listen address |
| `DOCTOR_SERVICE_ADDR` | `localhost:50051` | Doctor Service gRPC address |

### Notification Service

| Variable | Default | Description |
|---|---|---|
| `NATS_URL` | `nats://localhost:4222` | NATS server URL |

---

## 5. Infrastructure Setup

### Start PostgreSQL

```bash
# Create two separate databases — one per service
docker run -d --name pg \
  -e POSTGRES_PASSWORD=postgres \
  -p 5432:5432 \
  postgres:16-alpine

# Wait a few seconds, then create the databases
docker exec -it pg psql -U postgres -c "CREATE DATABASE doctors;"
docker exec -it pg psql -U postgres -c "CREATE DATABASE appointments;"
```

### Start NATS

```bash
docker run -d --name nats \
  -p 4222:4222 \
  nats:latest
```

---

## 6. First-Time Dependency Setup

Run once from the project root (requires internet access):

```bash
chmod +x setup.sh
./setup.sh
```

This runs `go mod tidy` in each service directory to download and verify all modules.

---

## 7. Migration Instructions

Migrations run **automatically on service startup** (before the gRPC server accepts requests). No manual steps are needed in normal operation.

### Manual apply / rollback (using golang-migrate CLI)

```bash
# Install the CLI
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Apply all up-migrations for Doctor Service
migrate -path doctor-service/migrations \
        -database "postgres://postgres:postgres@localhost:5432/doctors?sslmode=disable" \
        up

# Roll back one step (tested during defense)
migrate -path doctor-service/migrations \
        -database "postgres://postgres:postgres@localhost:5432/doctors?sslmode=disable" \
        down 1

# Same for Appointment Service
migrate -path appointment-service/migrations \
        -database "postgres://postgres:postgres@localhost:5432/appointments?sslmode=disable" \
        up

migrate -path appointment-service/migrations \
        -database "postgres://postgres:postgres@localhost:5432/appointments?sslmode=disable" \
        down 1
```

---

## 8. Service Startup Order

Start in this order (each in its own terminal):

```bash
# Terminal 1 — infrastructure (if not already running)
docker start pg nats

# Terminal 2 — Doctor Service (must start before Appointment Service)
cd doctor-service
DATABASE_URL="postgres://postgres:postgres@localhost:5432/doctors?sslmode=disable" \
NATS_URL="nats://localhost:4222" \
go run ./cmd/doctor-service

# Terminal 3 — Appointment Service
cd appointment-service
DATABASE_URL="postgres://postgres:postgres@localhost:5432/appointments?sslmode=disable" \
NATS_URL="nats://localhost:4222" \
DOCTOR_SERVICE_ADDR="localhost:50051" \
go run ./cmd/appointment-service

# Terminal 4 — Notification Service
cd notification-service
NATS_URL="nats://localhost:4222" \
go run ./cmd/notification-service
```

**Why this order:** The Appointment Service dials the Doctor Service gRPC endpoint at startup, so the Doctor Service must be reachable first. NATS must be running before the Notification Service starts (it retries with backoff).

---

## 9. Event Contract

| Subject | Publisher | Trigger | JSON Fields |
|---|---|---|---|
| `doctors.created` | Doctor Service | `CreateDoctor` succeeds | `event_type`, `occurred_at`, `id`, `full_name`, `specialization`, `email` |
| `appointments.created` | Appointment Service | `CreateAppointment` succeeds | `event_type`, `occurred_at`, `id`, `title`, `doctor_id`, `status` |
| `appointments.status_updated` | Appointment Service | `UpdateAppointmentStatus` succeeds | `event_type`, `occurred_at`, `id`, `old_status`, `new_status` |

### Example payloads

```json
// doctors.created
{
  "event_type": "doctors.created",
  "occurred_at": "2026-05-01T10:23:44Z",
  "id": "d1a2b3c4-...",
  "full_name": "Dr. Aisha Seitkali",
  "specialization": "Cardiology",
  "email": "a.seitkali@clinic.kz"
}

// appointments.created
{
  "event_type": "appointments.created",
  "occurred_at": "2026-05-01T10:24:01Z",
  "id": "e5f6g7h8-...",
  "title": "Initial cardiac consultation",
  "doctor_id": "d1a2b3c4-...",
  "status": "new"
}

// appointments.status_updated
{
  "event_type": "appointments.status_updated",
  "occurred_at": "2026-05-01T10:25:10Z",
  "id": "e5f6g7h8-...",
  "old_status": "new",
  "new_status": "in_progress"
}
```

---

## 10. Notification Service

The Notification Service is a pure subscriber — it has no gRPC server, no HTTP server, and no database. On startup it connects to NATS and subscribes to all three subjects. If NATS is unavailable it retries with exponential backoff (1 s → 2 s → 4 s → … up to 6 attempts) then exits with a non-zero code.

Each received message is deserialized from JSON and printed as a single JSON line to stdout:

```json
{"time":"2026-05-01T10:23:44Z","subject":"doctors.created","event":{"event_type":"doctors.created","occurred_at":"2026-05-01T10:23:44Z","id":"doc-1","full_name":"Dr. Aisha Seitkali","specialization":"Cardiology","email":"a.seitkali@clinic.kz"}}
```

On `SIGTERM` / `SIGINT` it drains in-flight messages, closes the NATS connection, and exits with code 0.

---

## 11. grpcurl Commands & Expected Notification Service Output

### Create a Doctor

```bash
grpcurl -plaintext -d '{
  "full_name": "Dr. Aisha Seitkali",
  "specialization": "Cardiology",
  "email": "a.seitkali@clinic.kz"
}' localhost:50051 doctor.DoctorService/CreateDoctor
```

**Expected Notification Service stdout:**
```json
{"time":"<RFC3339>","subject":"doctors.created","event":{"email":"a.seitkali@clinic.kz","event_type":"doctors.created","full_name":"Dr. Aisha Seitkali","id":"<uuid>","occurred_at":"<RFC3339>","specialization":"Cardiology"}}
```

---

### Create an Appointment

```bash
grpcurl -plaintext -d '{
  "title": "Initial cardiac consultation",
  "description": "First visit",
  "doctor_id": "<doctor_id_from_above>"
}' localhost:50052 appointment.AppointmentService/CreateAppointment
```

**Expected Notification Service stdout:**
```json
{"time":"<RFC3339>","subject":"appointments.created","event":{"doctor_id":"<id>","event_type":"appointments.created","id":"<uuid>","occurred_at":"<RFC3339>","status":"new","title":"Initial cardiac consultation"}}
```

---

### Update Appointment Status

```bash
grpcurl -plaintext -d '{
  "id": "<appointment_id>",
  "status": "in_progress"
}' localhost:50052 appointment.AppointmentService/UpdateAppointmentStatus
```

**Expected Notification Service stdout:**
```json
{"time":"<RFC3339>","subject":"appointments.status_updated","event":{"event_type":"appointments.status_updated","id":"<id>","new_status":"in_progress","occurred_at":"<RFC3339>","old_status":"new"}}
```

---

### Other Commands (unchanged from Assignment 2)

```bash
# Get doctor by ID
grpcurl -plaintext -d '{"id": "<id>"}' localhost:50051 doctor.DoctorService/GetDoctor

# List all doctors
grpcurl -plaintext -d '{}' localhost:50051 doctor.DoctorService/ListDoctors

# Get appointment by ID
grpcurl -plaintext -d '{"id": "<id>"}' localhost:50052 appointment.AppointmentService/GetAppointment

# List all appointments
grpcurl -plaintext -d '{}' localhost:50052 appointment.AppointmentService/ListAppointments
```

---

## 12. Consistency Trade-offs

### What happens when the broker is unavailable

- **Doctor / Appointment Services:** If NATS is down at startup, the service logs a warning and continues with a `NoopPublisher`. All RPCs succeed normally; events are simply not published.
- **Notification Service:** Retries with exponential backoff; exits with non-zero code if NATS is unreachable after all retries.
- **Mid-RPC broker failure:** If NATS becomes unavailable during an RPC, the publish call returns an error that is logged with full context. The gRPC response is returned successfully — broker publishing never blocks the RPC response.

### Which events can be lost

Because publishing is best-effort (fire-and-forget), a process crash between the DB `COMMIT` and the `Publish()` call will silently drop the event. The database will have the new row, but the Notification Service will never see it.

### How durable delivery would improve reliability

| Approach | Mechanism | Guarantee |
|---|---|---|
| **Outbox Pattern** | Write the event to an `outbox` table inside the same DB transaction, then have a background worker relay it to the broker | Atomic DB commit guarantees the event is never lost; at-least-once delivery |
| **NATS JetStream** | Persistent subjects with consumer acknowledgement and replay | Broker-side durability + re-delivery on failure |
| **RabbitMQ publisher confirms** | Broker ACKs each message after writing to durable queue | Ensures the broker received and persisted the message before the publisher proceeds |

---

## 13. Project Structure

```
ap2-assignment3/
├── doctor-service/
│   ├── cmd/doctor-service/main.go
│   ├── internal/
│   │   ├── model/doctor.go
│   │   ├── repository/
│   │   │   ├── doctor_repository.go          ← interface
│   │   │   └── postgres_doctor_repository.go ← PostgreSQL impl
│   │   ├── usecase/doctor_usecase.go
│   │   ├── event/publisher.go                ← EventPublisher interface + NATS impl
│   │   ├── transport/grpc/handler.go
│   │   └── app/app.go
│   ├── migrations/
│   │   ├── 000001_create_doctors.up.sql
│   │   └── 000001_create_doctors.down.sql
│   ├── proto/
│   │   ├── doctor.proto
│   │   ├── doctor.pb.go
│   │   └── doctor_grpc.pb.go
│   └── go.mod
├── appointment-service/
│   ├── cmd/appointment-service/main.go
│   ├── internal/
│   │   ├── model/appointment.go
│   │   ├── repository/
│   │   │   ├── appointment_repository.go          ← interface
│   │   │   └── postgres_appointment_repository.go ← PostgreSQL impl
│   │   ├── usecase/appointment_usecase.go
│   │   ├── event/publisher.go
│   │   ├── client/
│   │   │   ├── doctor_client.go     ← interface
│   │   │   └── grpc_doctor_client.go
│   │   ├── transport/grpc/handler.go
│   │   └── app/app.go
│   ├── migrations/
│   │   ├── 000001_create_appointments.up.sql
│   │   └── 000001_create_appointments.down.sql
│   ├── proto/
│   └── go.mod
├── notification-service/
│   ├── cmd/notification-service/main.go
│   ├── internal/subscriber/subscriber.go
│   └── go.mod
├── setup.sh
└── README.md
```
