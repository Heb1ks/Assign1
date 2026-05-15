package cache

import "context"

// CacheRepository — инфраструктурный интерфейс для кэширования апоинтментов.
type CacheRepository interface {
	// GetAppointment достаёт апоинтмент из кэша по ID (Cache-Aside).
	GetAppointment(ctx context.Context, id string) (string, error)

	// SetAppointment кэширует апоинтмент.
	SetAppointment(ctx context.Context, id string, data string, ttlSeconds int) error

	// GetAppointmentsList достаёт закэшированный список всех апоинтментов.
	GetAppointmentsList(ctx context.Context) (string, error)

	// SetAppointmentsList кэширует список всех апоинтментов.
	SetAppointmentsList(ctx context.Context, data string, ttlSeconds int) error

	// InvalidateAppointments удаляет ключ appointments:list.
	InvalidateAppointments(ctx context.Context) error
}

// Ключи:
//   appointment:<id>
//   appointments:list
