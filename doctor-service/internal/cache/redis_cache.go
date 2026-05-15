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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("[WARN] Redis unavailable at %s: %v — continuing without cache", redisURL, err)
		_ = client.Close()
		return &RedisCacheRepository{client: nil, ttl: ttlSeconds}
	}

	log.Printf("[INFO] Redis connected: %s (TTL=%ds)", redisURL, ttlSeconds)
	return &RedisCacheRepository{client: client, ttl: ttlSeconds}
}

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

func (r *RedisCacheRepository) SetDoctor(ctx context.Context, id string, data string, ttlSeconds int) error {
	if r.client == nil {
		return nil
	}
	key := fmt.Sprintf("doctor:%s", id)
	if err := r.client.Set(ctx, key, data, time.Duration(ttlSeconds)*time.Second).Err(); err != nil {
		log.Printf("[WARN] Redis SET %s: %v", key, err)
	}
	log.Printf("[DEBUG] Cache SET: %s (TTL=%ds)", key, ttlSeconds)
	return nil
}

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

func (r *RedisCacheRepository) InvalidateDoctors(ctx context.Context) error {
	if r.client == nil {
		return nil
	}
	if err := r.client.Del(ctx, "doctors:list").Err(); err != nil {
		log.Printf("[WARN] Redis DEL doctors:list: %v", err)
	}
	log.Printf("[DEBUG] Cache INVALIDATE: doctors:list")
	return nil
}

func (r *RedisCacheRepository) Close() error {
	if r.client == nil {
		return nil
	}
	return r.client.Close()
}
