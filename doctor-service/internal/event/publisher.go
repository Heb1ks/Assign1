package event

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"
)

// EventPublisher - интерфейс для публикации доменных событий.
// Благодаря интерфейсу use case не зависит от конкретного брокера.
type EventPublisher interface {
	Publish(ctx context.Context, subject string, payload any) error
	Close()
}

// DoctorCreatedEvent - payload события doctors.created
type DoctorCreatedEvent struct {
	EventType      string `json:"event_type"`
	OccurredAt     string `json:"occurred_at"`
	ID             string `json:"id"`
	FullName       string `json:"full_name"`
	Specialization string `json:"specialization"`
	Email          string `json:"email"`
}

// NATSPublisher - реализация EventPublisher поверх NATS Core.
type NATSPublisher struct {
	nc *nats.Conn
}

func NewNATSPublisher(url string) (*NATSPublisher, error) {
	nc, err := nats.Connect(url,
		nats.Name("doctor-service"),
		nats.MaxReconnects(5),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	return &NATSPublisher{nc: nc}, nil
}

func (p *NATSPublisher) Publish(_ context.Context, subject string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal event %q: %w", subject, err)
	}
	if err := p.nc.Publish(subject, data); err != nil {
		return fmt.Errorf("nats publish %q: %w", subject, err)
	}
	return nil
}

func (p *NATSPublisher) Close() {
	if err := p.nc.Drain(); err != nil {
		log.Printf("nats drain: %v", err)
	}
}

// NoopPublisher - заглушка, используется когда NATS недоступен при старте.
type NoopPublisher struct{}

func (NoopPublisher) Publish(_ context.Context, subject string, _ any) error {
	log.Printf("[noop-publisher] would publish to %q (broker unavailable)", subject)
	return nil
}
func (NoopPublisher) Close() {}

// NewDoctorCreatedEvent - конструктор события.
func NewDoctorCreatedEvent(id, fullName, specialization, email string) DoctorCreatedEvent {
	return DoctorCreatedEvent{
		EventType:      "doctors.created",
		OccurredAt:     time.Now().UTC().Format(time.RFC3339),
		ID:             id,
		FullName:       fullName,
		Specialization: specialization,
		Email:          email,
	}
}
