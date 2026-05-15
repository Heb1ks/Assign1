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
<<<<<<< HEAD
	ttl    int
}

// NewRedisCacheRepository подключается к Redis.
// При недоступности Redis возвращает репозиторий с nil-клиентом —
// все операции становятся no-op, сервис продолжает работать.
func NewRedisCacheRepository(redisURL string, ttlSeconds int) *RedisCacheRepository {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		log.Printf("[WARN] Redis: cannot parse REDIS_URL %q: %v — caching disabled", redisURL, err)
		return &RedisCacheRepository{client: nil, ttl: ttlSeconds}
	}
	opt.PoolSize = 10
	opt.MinIdleConns = 5
	client := redis.NewClient(opt)

=======
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
>>>>>>> e724c215eb318dd0da60e29464bf279ea24a307b
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
<<<<<<< HEAD
		log.Printf("[WARN] Redis unavailable at %s: %v — continuing without cache", redisURL, err)
		_ = client.Close()
		return &RedisCacheRepository{client: nil, ttl: ttlSeconds}
	}

	log.Printf("[INFO] Redis connected: %s (TTL=%ds)", redisURL, ttlSeconds)
	return &RedisCacheRepository{client: client, ttl: ttlSeconds}
}

=======
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
>>>>>>> e724c215eb318dd0da60e29464bf279ea24a307b
func (r *RedisCacheRepository) GetDoctor(ctx context.Context, id string) (string, error) {
	if r.client == nil {
		return "", fmt.Errorf("cache unavailable")
	}
<<<<<<< HEAD
=======

>>>>>>> e724c215eb318dd0da60e29464bf279ea24a307b
	key := fmt.Sprintf("doctor:%s", id)
	val, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", fmt.Errorf("cache miss")
	}
	if err != nil {
		log.Printf("[WARN] Redis GET %s: %v", key, err)
		return "", fmt.Errorf("cache error")
	}
<<<<<<< HEAD
=======

>>>>>>> e724c215eb318dd0da60e29464bf279ea24a307b
	log.Printf("[DEBUG] Cache HIT: %s", key)
	return val, nil
}

<<<<<<< HEAD
func (r *RedisCacheRepository) SetDoctor(ctx context.Context, id string, data string, ttlSeconds int) error {
	if r.client == nil {
		return nil
	}
	key := fmt.Sprintf("doctor:%s", id)
	if err := r.client.Set(ctx, key, data, time.Duration(ttlSeconds)*time.Second).Err(); err != nil {
		log.Printf("[WARN] Redis SET %s: %v", key, err)
	}
=======
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

>>>>>>> e724c215eb318dd0da60e29464bf279ea24a307b
	log.Printf("[DEBUG] Cache SET: %s (TTL=%ds)", key, ttlSeconds)
	return nil
}

<<<<<<< HEAD
func (r *RedisCacheRepository) GetDoctorsList(ctx context.Context) (string, error) {
	if r.client == nil {
		return "", fmt.Errorf("cache unavailable")
	}
	val, err := r.client.Get(ctx, "doctors:list").Result()
	if err == redis.Nil {
		return "", fmt.Errorf("cache miss")
	}
	if err != nil {
		log.Printf("[WARN] Redis GET doctors:list: %v", err)
		return "", fmt.Errorf("cache error")
	}
	log.Printf("[DEBUG] Cache HIT: doctors:list")
	return val, nil
}

func (r *RedisCacheRepository) SetDoctorsList(ctx context.Context, data string, ttlSeconds int) error {
	if r.client == nil {
		return nil
	}
	if err := r.client.Set(ctx, "doctors:list", data, time.Duration(ttlSeconds)*time.Second).Err(); err != nil {
		log.Printf("[WARN] Redis SET doctors:list: %v", err)
	}
	log.Printf("[DEBUG] Cache SET: doctors:list (TTL=%ds)", ttlSeconds)
	return nil
}

=======
// InvalidateDoctors инвалидирует кэш списка врачей при CreateDoctor
>>>>>>> e724c215eb318dd0da60e29464bf279ea24a307b
func (r *RedisCacheRepository) InvalidateDoctors(ctx context.Context) error {
	if r.client == nil {
		return nil
	}
<<<<<<< HEAD
	if err := r.client.Del(ctx, "doctors:list").Err(); err != nil {
		log.Printf("[WARN] Redis DEL doctors:list: %v", err)
	}
	log.Printf("[DEBUG] Cache INVALIDATE: doctors:list")
	return nil
}

=======

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
>>>>>>> e724c215eb318dd0da60e29464bf279ea24a307b
func (r *RedisCacheRepository) Close() error {
	if r.client == nil {
		return nil
	}
	return r.client.Close()
}
