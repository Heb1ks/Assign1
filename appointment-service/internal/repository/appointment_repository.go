package repository

import (
	"appointment-service/internal/model"
	"context"
)

type AppointmentRepository interface {
	Create(ctx context.Context, a *model.Appointment) error
	GetByID(ctx context.Context, id string) (*model.Appointment, error)
	GetAll(ctx context.Context) ([]*model.Appointment, error)
	Update(ctx context.Context, a *model.Appointment) error
}
