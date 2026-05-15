package usecase

import (
	"appointment-service/internal/cache"
	"appointment-service/internal/client"
	"appointment-service/internal/event"
	"appointment-service/internal/model"
	"appointment-service/internal/repository"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
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
	publisher    event.EventPublisher
	cache        cache.CacheRepository
	cacheTTL     int
}

func NewAppointmentUseCase(
	repo repository.AppointmentRepository,
	dc client.DoctorClient,
	pub event.EventPublisher,
	cacheRepo cache.CacheRepository,
) AppointmentUseCase {
	ttl := 60
	if v := os.Getenv("CACHE_TTL_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			ttl = n
		}
	}
	return &appointmentUseCase{
		repo:         repo,
		doctorClient: dc,
		publisher:    pub,
		cache:        cacheRepo,
		cacheTTL:     ttl,
	}
}

// CreateAppointment - Write-Around: пишем только в БД, список инвалидируем.
func (uc *appointmentUseCase) CreateAppointment(ctx context.Context, title, description, doctorID string) (*model.Appointment, error) {
	if title == "" {
		return nil, errors.New("title is required")
	}
	if doctorID == "" {
		return nil, errors.New("doctor_id is required")
	}

	exists, err := uc.doctorClient.DoctorExists(ctx, doctorID)
	if err != nil {
		return nil, fmt.Errorf("cannot verify doctor — Doctor Service unavailable: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("doctor %q does not exist", doctorID)
	}

	now := time.Now()
	a := &model.Appointment{
		ID:          uuid.New().String(),
		Title:       title,
		Description: description,
		DoctorID:    doctorID,
		Status:      model.StatusNew,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := uc.repo.Create(ctx, a); err != nil {
		return nil, fmt.Errorf("create appointment: %w", err)
	}

	// Write-Around: инвалидируем список, индивидуальный ключ не трогаем.
	if cerr := uc.cache.InvalidateAppointments(ctx); cerr != nil {
		log.Printf("[WARN] cache InvalidateAppointments: %v", cerr)
	}

	ev := event.NewAppointmentCreatedEvent(a.ID, a.Title, a.DoctorID, string(a.Status))
	if pubErr := uc.publisher.Publish(ctx, "appointments.created", ev); pubErr != nil {
		log.Printf("[WARN] publish appointments.created for %s: %v", a.ID, pubErr)
	}

	return a, nil
}

// GetAppointmentByID - Cache-Aside.
func (uc *appointmentUseCase) GetAppointmentByID(ctx context.Context, id string) (*model.Appointment, error) {
	if cached, err := uc.cache.GetAppointment(ctx, id); err == nil {
		var a model.Appointment
		if err := json.Unmarshal([]byte(cached), &a); err == nil {
			return &a, nil
		}
	}

	a, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("get appointment: %w", err)
	}

	go func() {
		if data, merr := json.Marshal(a); merr == nil {
			if cerr := uc.cache.SetAppointment(context.Background(), id, string(data), uc.cacheTTL); cerr != nil {
				log.Printf("[WARN] cache SetAppointment %s: %v", id, cerr)
			}
		}
	}()

	return a, nil
}

// ListAppointments - Cache-Aside на ключ appointments:list.
func (uc *appointmentUseCase) ListAppointments(ctx context.Context) ([]*model.Appointment, error) {
	if cached, err := uc.cache.GetAppointmentsList(ctx); err == nil {
		var list []*model.Appointment
		if err := json.Unmarshal([]byte(cached), &list); err == nil {
			return list, nil
		}
	}

	list, err := uc.repo.GetAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("list appointments: %w", err)
	}

	go func() {
		if data, merr := json.Marshal(list); merr == nil {
			if cerr := uc.cache.SetAppointmentsList(context.Background(), string(data), uc.cacheTTL); cerr != nil {
				log.Printf("[WARN] cache SetAppointmentsList: %v", cerr)
			}
		}
	}()

	return list, nil
}

// UpdateStatus - Write-Through: обновляем БД, затем кэшируем новое состояние
// и инвалидируем список.
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

	// Write-Through: обновляем кэш и инвалидируем список.
	if data, merr := json.Marshal(a); merr == nil {
		if cerr := uc.cache.SetAppointment(ctx, id, string(data), uc.cacheTTL); cerr != nil {
			log.Printf("[WARN] cache SetAppointment %s: %v", id, cerr)
		}
	}
	if cerr := uc.cache.InvalidateAppointments(ctx); cerr != nil {
		log.Printf("[WARN] cache InvalidateAppointments: %v", cerr)
	}

	ev := event.NewAppointmentStatusUpdatedEvent(a.ID, string(oldStatus), string(newStatus), a.DoctorID)
	if pubErr := uc.publisher.Publish(ctx, "appointments.status_updated", ev); pubErr != nil {
		log.Printf("[WARN] publish appointments.status_updated for %s: %v", a.ID, pubErr)
	}

	return a, nil
}
