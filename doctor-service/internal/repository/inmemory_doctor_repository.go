package repository

import (
	"context"
	"doctor-service/internal/model"
	"errors"
	"sync"
)

type InMemoryDoctorRepository struct {
	mu      sync.RWMutex
	doctors map[string]*model.Doctor
}

func NewInMemoryDoctorRepository() *InMemoryDoctorRepository {
	return &InMemoryDoctorRepository{doctors: make(map[string]*model.Doctor)}
}

func (r *InMemoryDoctorRepository) Create(ctx context.Context, d *model.Doctor) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.doctors[d.ID] = d
	return nil
}

func (r *InMemoryDoctorRepository) GetByID(ctx context.Context, id string) (*model.Doctor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.doctors[id]
	if !ok {
		return nil, errors.New("doctor not found")
	}
	return d, nil
}

func (r *InMemoryDoctorRepository) GetAll(ctx context.Context) ([]*model.Doctor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*model.Doctor, 0, len(r.doctors))
	for _, d := range r.doctors {
		result = append(result, d)
	}
	return result, nil
}

func (r *InMemoryDoctorRepository) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, d := range r.doctors {
		if d.Email == email {
			return true, nil
		}
	}
	return false, nil
}
