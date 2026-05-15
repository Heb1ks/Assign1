package event

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"
)

type EventPublisher interface {
	Publish(ctx context.Context, subject string, payload any) error
	Close()
}

type AppointmentCreatedEvent struct {
	EventType  string `json:"event_type"`
	OccurredAt string `json:"occurred_at"`
	ID         string `json:"id"`
	Title      string `json:"title"`
	DoctorID   string `json:"doctor_id"`
	Status     string `json:"status"`
}

type AppointmentStatusUpdatedEvent struct {
	EventType  string `json:"event_type"`
	OccurredAt string `json:"occurred_at"`
	ID         string `json:"id"`
	OldStatus  string `json:"old_status"`
	NewStatus  string `json:"new_status"`
	DoctorID   string `json:"doctor_id"`
}

type NATSPublisher struct {
	nc *nats.Conn
}

func NewNATSPublisher(url string) (*NATSPublisher, error) {
	nc, err := nats.Connect(url,
		nats.Name("appointment-service"),
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

type NoopPublisher struct{}

func (NoopPublisher) Publish(_ context.Context, subject string, _ any) error {
	log.Printf("[noop] would publish to %q", subject)
	return nil
}
func (NoopPublisher) Close() {}

func NewAppointmentCreatedEvent(id, title, doctorID, status string) AppointmentCreatedEvent {
	return AppointmentCreatedEvent{
		EventType:  "appointments.created",
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
		ID:         id, Title: title, DoctorID: doctorID, Status: status,
	}
}

func NewAppointmentStatusUpdatedEvent(id, oldStatus, newStatus, doctorID string) AppointmentStatusUpdatedEvent {
	return AppointmentStatusUpdatedEvent{
		EventType:  "appointments.status_updated",
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
		ID:         id, OldStatus: oldStatus, NewStatus: newStatus, DoctorID: doctorID,
	}
}
