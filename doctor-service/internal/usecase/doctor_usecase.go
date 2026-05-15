package usecase

import (
	"context"
	"doctor-service/internal/cache"
	"doctor-service/internal/event"
	"doctor-service/internal/model"
	"doctor-service/internal/repository"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/google/uuid"
)

type DoctorUseCase interface {
	CreateDoctor(ctx context.Context, fullName, specialization, email string) (*model.Doctor, error)
	GetDoctorByID(ctx context.Context, id string) (*model.Doctor, error)
	ListDoctors(ctx context.Context) ([]*model.Doctor, error)
}

type doctorUseCase struct {
	repo      repository.DoctorRepository
	cache     cache.CacheRepository
	publisher event.EventPublisher
	cacheTTL  int
}

func NewDoctorUseCase(
	repo repository.DoctorRepository,
	cacheRepo cache.CacheRepository,
	pub event.EventPublisher,
) DoctorUseCase {
	ttl := 60
	if v := os.Getenv("CACHE_TTL_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			ttl = n
		}
	}
	return &doctorUseCase{
		repo:      repo,
		cache:     cacheRepo,
		publisher: pub,
		cacheTTL:  ttl,
	}
}

// CreateDoctor - Write-Through: сохраняем в БД, затем кэшируем врача и
// инвалидируем список (doctors:list).
func (uc *doctorUseCase) CreateDoctor(ctx context.Context, fullName, specialization, email string) (*model.Doctor, error) {
	if fullName == "" {
		return nil, errors.New("full_name is required")
	}
	if email == "" {
		return nil, errors.New("email is required")
	}

	exists, err := uc.repo.ExistsByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("check email: %w", err)
	}
	if exists {
		return nil, repository.ErrEmailExists
	}

	doctor := &model.Doctor{
		ID:             uuid.New().String(),
		FullName:       fullName,
		Specialization: specialization,
		Email:          email,
	}
	if err := uc.repo.Create(ctx, doctor); err != nil {
		if errors.Is(err, repository.ErrEmailExists) {
			return nil, repository.ErrEmailExists
		}
		return nil, fmt.Errorf("create doctor: %w", err)
	}

	// Write-Through: кэшируем нового врача сразу после записи в БД.
	if data, merr := json.Marshal(doctor); merr == nil {
		if cerr := uc.cache.SetDoctor(ctx, doctor.ID, string(data), uc.cacheTTL); cerr != nil {
			log.Printf("[WARN] cache SetDoctor %s: %v", doctor.ID, cerr)
		}
	}
	// инвалидируем
	if cerr := uc.cache.InvalidateDoctors(ctx); cerr != nil {
		log.Printf("[WARN] cache InvalidateDoctors: %v", cerr)
	}

	// Публикуем событие (best-effort).
	ev := event.NewDoctorCreatedEvent(doctor.ID, doctor.FullName, doctor.Specialization, doctor.Email)
	if pubErr := uc.publisher.Publish(ctx, "doctors.created", ev); pubErr != nil {
		log.Printf("[WARN] publish doctors.created for %s: %v", doctor.ID, pubErr)
	}

	return doctor, nil
}

// GetDoctorByID - Cache-Aside: сначала кэш, при промахе идём в БД и кэшируем результат.
func (uc *doctorUseCase) GetDoctorByID(ctx context.Context, id string) (*model.Doctor, error) {
	if cached, err := uc.cache.GetDoctor(ctx, id); err == nil {
		var doctor model.Doctor
		if err := json.Unmarshal([]byte(cached), &doctor); err == nil {
			return &doctor, nil
		}
	}

	doctor, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("get doctor: %w", err)
	}

	// Сохраняем в кэш асинхронно - не блокируем ответ клиенту.
	go func() {
		if data, merr := json.Marshal(doctor); merr == nil {
			if cerr := uc.cache.SetDoctor(context.Background(), id, string(data), uc.cacheTTL); cerr != nil {
				log.Printf("[WARN] cache SetDoctor %s: %v", id, cerr)
			}
		}
	}()

	return doctor, nil
}

// ListDoctors - Cache-Aside: пробуем вернуть из кэша (ключ doctors:list),
// при промахе идём в БД и кэшируем результат.
func (uc *doctorUseCase) ListDoctors(ctx context.Context) ([]*model.Doctor, error) {
	if cached, err := uc.cache.GetDoctorsList(ctx); err == nil {
		var doctors []*model.Doctor
		if err := json.Unmarshal([]byte(cached), &doctors); err == nil {
			return doctors, nil
		}
	}

	doctors, err := uc.repo.GetAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("list doctors: %w", err)
	}

	// Кэшируем список асинхронно.
	go func() {
		if data, merr := json.Marshal(doctors); merr == nil {
			if cerr := uc.cache.SetDoctorsList(context.Background(), string(data), uc.cacheTTL); cerr != nil {
				log.Printf("[WARN] cache SetDoctorsList: %v", cerr)
			}
		}
	}()

	return doctors, nil
}
