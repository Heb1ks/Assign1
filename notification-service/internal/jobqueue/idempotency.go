package jobqueue

import (
	"crypto/sha256"
	"fmt"
	"time"
)

// GenerateIdempotencyKey создает детерминированный SHA-256 ключ
// Формат: SHA256(eventType:id:timestamp)
func GenerateIdempotencyKey(eventType string, id string, timestamp time.Time) string {
	// Используем только дату (в формате YYYYMMDD) для снижения хеша
	// Это обеспечивает детерминированность при переподключении в тот же день
	dateStr := timestamp.Format("20060102")
	input := fmt.Sprintf("%s:%s:%s", eventType, id, dateStr)
	hash := sha256.Sum256([]byte(input))
	return fmt.Sprintf("idempotency:%x", hash[:16]) // Первые 16 байт хеша
}

const IdempotencyTTL = 24 * 60 * 60 // 24 часа в секундах
