package subscriber

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"notification-service/internal/subscriber/gateway"
	"notification-service/internal/subscriber/jobqueue"
	"notification-service/internal/subscriber/logger"
	"os"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

// Event — структура входящего события из NATS.
type Event struct {
	EventType      string    `json:"event_type"`
	OccurredAt     time.Time `json:"occurred_at"`
	ID             string    `json:"id"`
	OldStatus      string    `json:"old_status,omitempty"`
	NewStatus      string    `json:"new_status,omitempty"`
	FullName       string    `json:"full_name,omitempty"`
	Specialization string    `json:"specialization,omitempty"`
	Email          string    `json:"email,omitempty"`
	Title          string    `json:"title,omitempty"`
	DoctorID       string    `json:"doctor_id,omitempty"`
	Status         string    `json:"status,omitempty"`
}

// RedisIdempotencyStore реализует jobqueue.CacheRepository через Redis.
type RedisIdempotencyStore struct {
	client *redis.Client
}

func (rc *RedisIdempotencyStore) CheckIdempotencyKey(ctx context.Context, key string) (bool, error) {
	exists, err := rc.client.Exists(ctx, key).Result()
	return exists == 1, err
}

func (rc *RedisIdempotencyStore) SetIdempotencyKey(ctx context.Context, key string, ttlSeconds int) error {
	return rc.client.Set(ctx, key, "done", time.Duration(ttlSeconds)*time.Second).Err()
}

// Subscriber объединяет подписку на NATS и воркер-пул.
type Subscriber struct {
	conn   *nats.Conn
	logger *logger.JSONLogger
	pool   *jobqueue.WorkerPool
}

// New создаёт Subscriber. Читает REDIS_URL из окружения.
func New(natsURL string, numWorkers int) (*Subscriber, error) {
	nc, err := nats.Connect(natsURL)
	if err != nil {
		return nil, fmt.Errorf("NATS connect error: %w", err)
	}

	// Redis — читаем URL из REDIS_URL (не хардкодим).
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379"
	}
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		log.Printf("[WARN] Cannot parse REDIS_URL %q: %v — idempotency checks disabled", redisURL, err)
		opt = &redis.Options{Addr: "localhost:6379"}
	}
	redisClient := redis.NewClient(opt)

	ctx2, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := redisClient.Ping(ctx2).Err(); err != nil {
		log.Printf("[WARN] Redis unavailable for Notification Service — idempotency checks disabled")
	}

	store := &RedisIdempotencyStore{client: redisClient}
	jsonLogger := logger.New(os.Stdout)

	// GATEWAY_URL из окружения.
	gatewayURL := os.Getenv("GATEWAY_URL")
	if gatewayURL == "" {
		gatewayURL = "http://localhost:8080"
	}
	gw := gateway.NewNotificationGateway(gatewayURL)

	// WORKER_POOL_SIZE из окружения.
	pool := jobqueue.NewWorkerPool(numWorkers, jsonLogger, gw, store)
	pool.Start()

	return &Subscriber{
		conn:   nc,
		logger: jsonLogger,
		pool:   pool,
	}, nil
}

// Subscribe подписывается на все топики.
func (s *Subscriber) Subscribe() error {
	subjects := []string{
		"doctors.created",
		"appointments.created",
		"appointments.status_updated",
	}
	for _, subj := range subjects {
		_, err := s.conn.Subscribe(subj, s.handleMessage(subj))
		if err != nil {
			return fmt.Errorf("subscribe error for %s: %w", subj, err)
		}
		log.Printf("[INFO] Subscribed to %s", subj)
	}
	return nil
}

func (s *Subscriber) handleMessage(subject string) func(*nats.Msg) {
	return func(msg *nats.Msg) {
		var ev Event
		if err := json.Unmarshal(msg.Data, &ev); err != nil {
			s.logger.Error(fmt.Sprintf("unmarshal event from %s", subject), err)
			return
		}
		s.logger.Info("event received", map[string]interface{}{
			"subject": subject, "event_type": ev.EventType, "id": ev.ID,
		})

		job := &jobqueue.Job{
			EventType:     ev.EventType,
			OccurredAt:    ev.OccurredAt,
			ID:            ev.ID,
			AppointmentID: ev.ID,
			DoctorID:      ev.DoctorID,
			Status:        ev.Status,
			OldStatus:     ev.OldStatus,
			NewStatus:     ev.NewStatus,
			Data: map[string]string{
				"full_name":      ev.FullName,
				"specialization": ev.Specialization,
				"email":          ev.Email,
				"title":          ev.Title,
				"doctor_id":      ev.DoctorID,
			},
		}
		s.pool.Submit(job)
	}
}

// Drain выполняет graceful shutdown.
func (s *Subscriber) Drain() error {
	_ = s.conn.Drain()
	return s.pool.Stop()
}
