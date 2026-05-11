package middleware

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RateLimiter использует Redis Sliding Window Counter
// Лимит: 100 RPM (по умолчанию)
type RateLimiter struct {
	client      *redis.Client
	limitPerMin int // настраивается через env RATE_LIMIT_RPM
	window      int64
}

// NewRateLimiter конструктор с graceful degradation
func NewRateLimiter(redisAddr string, limitPerMin int) *RateLimiter {
	client := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		PoolSize: 5,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("[WARN] Rate Limiter: Redis unavailable at %s: %v - rate limiting disabled", redisAddr, err)
		return &RateLimiter{
			client:      nil,
			limitPerMin: limitPerMin,
			window:      60,
		}
	}

	log.Printf("[INFO] Rate Limiter initialized: %d RPM", limitPerMin)
	return &RateLimiter{
		client:      client,
		limitPerMin: limitPerMin,
		window:      60, // 60 секунд окно
	}
}

// UnaryServerInterceptor для всех unary RPC
func (rl *RateLimiter) UnaryServerInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	// Если Redis недоступен, пропускаем rate limiting
	if rl.client == nil {
		return handler(ctx, req)
	}

	// Ключ: глобальный лимит (в реальной системе использовать IP из peer metadata)
	clientID := "global"

	// Sliding Window Counter
	now := time.Now().Unix()
	windowStart := now - rl.window

	key := fmt.Sprintf("rate_limit:%s", clientID)

	// ZADD: добавить временную метку в отсортированный набор
	pipe := rl.client.Pipeline()
	pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", windowStart))
	cardCmd := pipe.ZCard(ctx, key)
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(now), Member: fmt.Sprintf("%d", now)})
	pipe.Expire(ctx, key, time.Duration(rl.window+1)*time.Second)

	if _, err := pipe.Exec(ctx); err != nil {
		log.Printf("[WARN] Rate Limiter: Redis error: %v - allowing request", err)
		return handler(ctx, req)
	}

	// Извлекаем количество requests за окно
	count, err := cardCmd.Result()
	if err != nil {
		return handler(ctx, req)
	}

	if count >= int64(rl.limitPerMin) {
		log.Printf("[WARN] Rate limit exceeded for %s: %d/%d RPM", clientID, count, rl.limitPerMin)
		return nil, status.Error(codes.ResourceExhausted, "rate limit exceeded: 100 requests per minute")
	}

	return handler(ctx, req)
}

// Close graceful shutdown
func (rl *RateLimiter) Close() error {
	if rl.client == nil {
		return nil
	}
	return rl.client.Close()
}
