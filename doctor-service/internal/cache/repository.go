package cache

import "context"

// CacheRepository — инфраструктурный интерфейс (Clean Architecture).
// Use-cases зависят только от этого интерфейса, Redis-импортов в них нет.
type CacheRepository interface {
	// GetDoctor достаёт врача из кэша по ID (Cache-Aside).
	GetDoctor(ctx context.Context, id string) (string, error)

	// SetDoctor кэширует одного врача.
	SetDoctor(ctx context.Context, id string, data string, ttlSeconds int) error

	// GetDoctorsList достаёт закэшированный список всех врачей.
	GetDoctorsList(ctx context.Context) (string, error)

	// SetDoctorsList кэширует список всех врачей.
	SetDoctorsList(ctx context.Context, data string, ttlSeconds int) error

	// InvalidateDoctors удаляет ключ doctors:list при создании/изменении врача.
	InvalidateDoctors(ctx context.Context) error

    // Close graceful shutdown
	Close() error
}

// Ключи:
//   doctor:<id>
//   doctors:list
