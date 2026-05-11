package repository

import (
	"context"
	"doctor-service/internal/model"
)

type DoctorRepository interface {
	Create(ctx context.Context, doctor *model.Doctor) error
	GetByID(ctx context.Context, id string) (*model.Doctor, error)
	GetAll(ctx context.Context) ([]*model.Doctor, error)
	ExistsByEmail(ctx context.Context, email string) (bool, error)
}
