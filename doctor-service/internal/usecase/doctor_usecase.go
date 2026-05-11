package usecase

import (
	"context"
	"doctor-service/internal/event"
	"doctor-service/internal/model"
	"doctor-service/internal/repository"
	"errors"
	"fmt"
	"log"

	"github.com/google/uuid"
)

type DoctorUseCase interface {
	CreateDoctor(ctx context.Context, fullName, specialization, email string) (*model.Doctor, error)
	GetDoctorByID(ctx context.Context, id string) (*model.Doctor, error)
	ListDoctors(ctx context.Context) ([]*model.Doctor, error)
}

type doctorUseCase struct {
	repo      repository.DoctorRepository
	publisher event.EventPublisher
}

// NewDoctorUseCase теперь принимает EventPublisher вторым аргументом.
func NewDoctorUseCase(repo repository.DoctorRepository, pub event.EventPublisher) DoctorUseCase {
	return &doctorUseCase{repo: repo, publisher: pub}
}

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

	// publixh event - best-effort, ошибка не ломает RPC
	ev := event.NewDoctorCreatedEvent(doctor.ID, doctor.FullName, doctor.Specialization, doctor.Email)
	if pubErr := uc.publisher.Publish(ctx, "doctors.created", ev); pubErr != nil {
		log.Printf("WARN: publish doctors.created failed for %s: %v", doctor.ID, pubErr)
	}

	return doctor, nil
}

func (uc *doctorUseCase) GetDoctorByID(ctx context.Context, id string) (*model.Doctor, error) {
	d, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("get doctor: %w", err)
	}
	return d, nil
}

func (uc *doctorUseCase) ListDoctors(ctx context.Context) ([]*model.Doctor, error) {
	return uc.repo.GetAll(ctx)
}
