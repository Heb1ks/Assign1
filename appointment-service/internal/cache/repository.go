package cache

import "context"

// CacheRepository — инфраструктурный интерфейс для appointment-service.
type CacheRepository interface {
	GetAppointment(ctx context.Context, id string) (string, error)
	SetAppointment(ctx context.Context, id string, data string, ttlSeconds int) error
	GetAppointmentsList(ctx context.Context) (string, error)
	SetAppointmentsList(ctx context.Context, data string, ttlSeconds int) error
	InvalidateAppointments(ctx context.Context) error
	Close() error
}