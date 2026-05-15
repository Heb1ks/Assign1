package jobqueue

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"
)

// IdempotencyTTL - 24 часа в секундах.
const IdempotencyTTL = 24 * 60 * 60

// CacheRepository - интерфейс хранилища идемпотентности (реализуется Redis).
type CacheRepository interface {
	CheckIdempotencyKey(ctx context.Context, key string) (bool, error)
	SetIdempotencyKey(ctx context.Context, key string, ttlSeconds int) error
}

// GenerateIdempotencyKey возвращает SHA-256 от "eventType:id:occurred_at(RFC3339)".
// Используем полный RFC3339 timestamp, а не только дату -
// это гарантирует уникальность при нескольких событиях за один день.
func GenerateIdempotencyKey(eventType string, id string, timestamp time.Time) string {
	input := fmt.Sprintf("%s:%s:%s", eventType, id, timestamp.UTC().Format(time.RFC3339Nano))
	hash := sha256.Sum256([]byte(input))
	return fmt.Sprintf("idempotency:%x", hash[:])
}
