package repository

import (
	"appointment-service/internal/model"
	"context"
	"errors"
	"sync"
)

type InMemoryAppointmentRepository struct {
	mu           sync.RWMutex
	appointments map[string]*model.Appointment
}

func NewInMemoryAppointmentRepository() *InMemoryAppointmentRepository {
	return &InMemoryAppointmentRepository{
		appointments: make(map[string]*model.Appointment),
	}
}

func (r *InMemoryAppointmentRepository) Create(ctx context.Context, a *model.Appointment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.appointments[a.ID] = a
	return nil
}

func (r *InMemoryAppointmentRepository) GetByID(ctx context.Context, id string) (*model.Appointment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.appointments[id]
	if !ok {
		return nil, errors.New("appointment not found")
	}
	return a, nil
}

func (r *InMemoryAppointmentRepository) GetAll(ctx context.Context) ([]*model.Appointment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*model.Appointment, 0, len(r.appointments))
	for _, a := range r.appointments {
		result = append(result, a)
	}
	return result, nil
}

func (r *InMemoryAppointmentRepository) Update(ctx context.Context, a *model.Appointment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.appointments[a.ID]; !ok {
		return errors.New("appointment not found")
	}
	r.appointments[a.ID] = a
	return nil
}
