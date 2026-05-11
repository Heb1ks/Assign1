package cache

import "context"

// CacheRepository — инфраструктурный интерфейс (Clean Architecture)
// Логика работы с Redis полностью скрыта, use-cases видят только этот интерфейс
type CacheRepository interface {
	// GetDoctor достает врача из кэша (Cache-Aside стратегия)
	GetDoctor(ctx context.Context, id string) (string, error)
	
	// SetDoctor кэширует врача (Write-Through при создании)
	SetDoctor(ctx context.Context, id string, data string, ttlSeconds int) error
	
	// InvalidateDoctors инвалидирует список врачей
	InvalidateDoctors(ctx context.Context) error
	
	// GetAppointment достает назначение из кэша
	GetAppointment(ctx context.Context, id string) (string, error)
	
	// SetAppointment кэширует назначение
	SetAppointment(ctx context.Context, id string, data string, ttlSeconds int) error
	
	// InvalidateAppointments инвалидирует список назначений
	InvalidateAppointments(ctx context.Context) error
	
	// CheckIdempotencyKey проверяет наличие ключа идемпотентности (для Notification Service)
	CheckIdempotencyKey(ctx context.Context, key string) (bool, error)
	
	// SetIdempotencyKey сохраняет ключ идемпотентности на 24 часа
	SetIdempotencyKey(ctx context.Context, key string, ttlSeconds int) error
	
	// Close graceful shutdown
	Close() error
}

// Формат ключей:
// doctor:{id}
// appointment:{id}
// doctors:list
// appointments:list
// idempotency:{sha256_hash}
