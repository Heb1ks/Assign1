package usecase

import (
	"appointment-service/internal/client"
	"appointment-service/internal/event"
	"appointment-service/internal/model"
	"appointment-service/internal/repository"
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
)

type AppointmentUseCase interface {
	CreateAppointment(ctx context.Context, title, description, doctorID string) (*model.Appointment, error)
	GetAppointmentByID(ctx context.Context, id string) (*model.Appointment, error)
	ListAppointments(ctx context.Context) ([]*model.Appointment, error)
	UpdateStatus(ctx context.Context, id string, status model.Status) (*model.Appointment, error)
}

type appointmentUseCase struct {
	repo         repository.AppointmentRepository
	doctorClient client.DoctorClient
	publisher    event.EventPublisher // ← новое
}

func NewAppointmentUseCase(
	repo repository.AppointmentRepository,
	dc client.DoctorClient,
	pub event.EventPublisher,
) AppointmentUseCase {
	return &appointmentUseCase{repo: repo, doctorClient: dc, publisher: pub}
}

func (uc *appointmentUseCase) CreateAppointment(ctx context.Context, title, description, doctorID string) (*model.Appointment, error) {
	if title == "" {
		return nil, errors.New("title is required")
	}
	if doctorID == "" {
		return nil, errors.New("doctor_id is required")
	}

	exists, err := uc.doctorClient.DoctorExists(ctx, doctorID)
	if err != nil {
		return nil, fmt.Errorf("cannot verify doctor - Doctor Service unavailable: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("doctor %q does not exist", doctorID)
	}

	now := time.Now()
	a := &model.Appointment{
		ID: uuid.New().String(), Title: title, Description: description,
		DoctorID: doctorID, Status: model.StatusNew,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := uc.repo.Create(ctx, a); err != nil {
		return nil, fmt.Errorf("create appointment: %w", err)
	}

	ev := event.NewAppointmentCreatedEvent(a.ID, a.Title, a.DoctorID, string(a.Status))
	if pubErr := uc.publisher.Publish(ctx, "appointments.created", ev); pubErr != nil {
		log.Printf("WARN: publish appointments.created failed for %s: %v", a.ID, pubErr)
	}

	return a, nil
}

func (uc *appointmentUseCase) GetAppointmentByID(ctx context.Context, id string) (*model.Appointment, error) {
	a, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("get appointment: %w", err)
	}
	return a, nil
}

func (uc *appointmentUseCase) ListAppointments(ctx context.Context) ([]*model.Appointment, error) {
	return uc.repo.GetAll(ctx)
}

func (uc *appointmentUseCase) UpdateStatus(ctx context.Context, id string, newStatus model.Status) (*model.Appointment, error) {
	if !newStatus.IsValid() {
		return nil, fmt.Errorf("invalid status %q", newStatus)
	}

	a, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("get appointment: %w", err)
	}

	if a.Status == model.StatusDone && newStatus == model.StatusNew {
		return nil, errors.New("transition from 'done' to 'new' is not allowed")
	}

	oldStatus := a.Status
	a.Status = newStatus
	a.UpdatedAt = time.Now()
	if err := uc.repo.Update(ctx, a); err != nil {
		return nil, fmt.Errorf("update appointment: %w", err)
	}

	ev := event.NewAppointmentStatusUpdatedEvent(a.ID, string(oldStatus), string(newStatus))
	if pubErr := uc.publisher.Publish(ctx, "appointments.status_updated", ev); pubErr != nil {
		log.Printf("WARN: publish appointments.status_updated failed for %s: %v", a.ID, pubErr)
	}

	return a, nil
}
