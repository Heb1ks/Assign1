package cache

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisCacheRepository struct {
	client *redis.Client
	ttl    int // TTL в секундах из env
}

// NewRedisCacheRepository конструктор с graceful degradation
func NewRedisCacheRepository(redisAddr string, ttlSeconds int) *RedisCacheRepository {
	client := redis.NewClient(&redis.Options{
		Addr:         redisAddr,
		PoolSize:     10,
		MinIdleConns: 5,
	})

	// Проверяем доступность Redis с таймаутом
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		// КРИТИЧНО: Redis недоступен, но не паникуем
		log.Printf("[WARN] Redis unavailable at %s: %v - continuing without cache", redisAddr, err)
		return &RedisCacheRepository{
			client: nil, // nil client означает \"работаем в режиме fallback\"
			ttl:    ttlSeconds,
		}
	}

	log.Printf("[INFO] Redis connected: %s (TTL=%ds)", redisAddr, ttlSeconds)
	return &RedisCacheRepository{
		client: client,
		ttl:    ttlSeconds,
	}
}

// GetDoctor Cache-Aside: если кэша нет, вернуть ошибку (caller сам идет в БД)
func (r *RedisCacheRepository) GetDoctor(ctx context.Context, id string) (string, error) {
	if r.client == nil {
		return "", fmt.Errorf("cache unavailable")
	}

	key := fmt.Sprintf("doctor:%s", id)
	val, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", fmt.Errorf("cache miss")
	}
	if err != nil {
		log.Printf("[WARN] Redis GET %s: %v", key, err)
		return "", fmt.Errorf("cache error")
	}

	log.Printf("[DEBUG] Cache HIT: %s", key)
	return val, nil
}

// SetDoctor Write-Through: сохраняем в кэш при создании врача
func (r *RedisCacheRepository) SetDoctor(ctx context.Context, id string, data string, ttlSeconds int) error {
	if r.client == nil {
		log.Printf("[WARN] Cache unavailable, skipping SetDoctor")
		return nil
	}

	key := fmt.Sprintf("doctor:%s", id)
	if err := r.client.Set(ctx, key, data, time.Duration(ttlSeconds)*time.Second).Err(); err != nil {
		log.Printf("[WARN] Redis SET %s: %v", key, err)
		return nil // Non-fatal: продолжаем работу даже если кэш упал
	}

	log.Printf("[DEBUG] Cache SET: %s (TTL=%ds)", key, ttlSeconds)
	return nil
}

// InvalidateDoctors инвалидирует кэш списка врачей при CreateDoctor
func (r *RedisCacheRepository) InvalidateDoctors(ctx context.Context) error {
	if r.client == nil {
		return nil
	}

	key := "doctors:list"
	if err := r.client.Del(ctx, key).Err(); err != nil {
		log.Printf("[WARN] Redis DEL %s: %v", key, err)
		return nil
	}

	log.Printf("[DEBUG] Cache INVALIDATE: %s", key)
	return nil
}

// GetAppointment Cache-Aside для назначений
func (r *RedisCacheRepository) GetAppointment(ctx context.Context, id string) (string, error) {
	if r.client == nil {
		return "", fmt.Errorf("cache unavailable")
	}

	key := fmt.Sprintf("appointment:%s", id)
	val, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", fmt.Errorf("cache miss")
	}
	if err != nil {
		log.Printf("[WARN] Redis GET %s: %v", key, err)
		return "", fmt.Errorf("cache error")
	}

	return val, nil
}

// SetAppointment Write-Through при создании назначения
func (r *RedisCacheRepository) SetAppointment(ctx context.Context, id string, data string, ttlSeconds int) error {
	if r.client == nil {
		log.Printf("[WARN] Cache unavailable, skipping SetAppointment")
		return nil
	}

	key := fmt.Sprintf("appointment:%s", id)
	if err := r.client.Set(ctx, key, data, time.Duration(ttlSeconds)*time.Second).Err(); err != nil {
		log.Printf("[WARN] Redis SET %s: %v", key, err)
		return nil
	}

	log.Printf("[DEBUG] Cache SET: %s (TTL=%ds)", key, ttlSeconds)
	return nil
}

// InvalidateAppointments инвалидирует список при UpdateAppointmentStatus
func (r *RedisCacheRepository) InvalidateAppointments(ctx context.Context) error {
	if r.client == nil {
		return nil
	}

	key := "appointments:list"
	if err := r.client.Del(ctx, key).Err(); err != nil {
		log.Printf("[WARN] Redis DEL %s: %v", key, err)
		return nil
	}

	log.Printf("[DEBUG] Cache INVALIDATE: %s", key)
	return nil
}

// CheckIdempotencyKey для Notification Service
func (r *RedisCacheRepository) CheckIdempotencyKey(ctx context.Context, key string) (bool, error) {
	if r.client == nil {
		return false, nil // fallback: всегда обрабатываем
	}

	exists, err := r.client.Exists(ctx, key).Result()
	if err != nil {
		log.Printf("[WARN] Redis EXISTS %s: %v", key, err)
		return false, nil
	}

	return exists == 1, nil
}

// SetIdempotencyKey сохраняет на 24 часа
func (r *RedisCacheRepository) SetIdempotencyKey(ctx context.Context, key string, ttlSeconds int) error {
	if r.client == nil {
		return nil
	}

	if err := r.client.Set(ctx, key, "1", time.Duration(ttlSeconds)*time.Second).Err(); err != nil {
		log.Printf("[WARN] Redis SET idempotency key %s: %v", key, err)
		return nil
	}

	return nil
}

// Close graceful shutdown
func (r *RedisCacheRepository) Close() error {
	if r.client == nil {
		return nil
	}
	return r.client.Close()
}
