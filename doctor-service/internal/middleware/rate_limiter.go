package middleware

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// RateLimiter реализует sliding-window counter на Redis.
// Алгоритм: ZADD каждого запроса с timestamp в отсортированный набор,
// ZREMRANGEBYSCORE убирает устаревшие записи за пределами окна,
// ZCARD - текущее количество запросов в окне.
// Ключ: rate_limit:<clientIP>
type RateLimiter struct {
	client      *redis.Client
	limitPerMin int
	window      int64 // 60 секунд
}

// NewRateLimiter создаёт rate limiter. Читает адрес из REDIS_URL.
// При недоступности Redis rate limiting отключается (graceful degradation).
func NewRateLimiter(redisURL string, limitPerMin int) *RateLimiter {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		log.Printf("[WARN] RateLimiter: cannot parse REDIS_URL: %v — rate limiting disabled", err)
		return &RateLimiter{client: nil, limitPerMin: limitPerMin, window: 60}
	}
	opt.PoolSize = 5
	client := redis.NewClient(opt)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("[WARN] RateLimiter: Redis unavailable: %v — rate limiting disabled", err)
		_ = client.Close()
		return &RateLimiter{client: nil, limitPerMin: limitPerMin, window: 60}
	}

	log.Printf("[INFO] RateLimiter initialized: %d RPM (sliding window)", limitPerMin)
	return &RateLimiter{client: client, limitPerMin: limitPerMin, window: 60}
}

// UnaryServerInterceptor - gRPC-перехватчик, применяется ко всем unary-методам.
func (rl *RateLimiter) UnaryServerInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	if rl.client == nil {
		return handler(ctx, req)
	}

	// Получаем IP клиента из gRPC peer-метаданных.
	clientIP := "unknown"
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		clientIP = p.Addr.String()
	}

	now := time.Now().UnixNano()
	windowStart := now - rl.window*int64(time.Second)
	key := fmt.Sprintf("rate_limit:%s", clientIP)

	pipe := rl.client.Pipeline()
	pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", windowStart))
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(now), Member: fmt.Sprintf("%d", now)})
	countCmd := pipe.ZCard(ctx, key)
	pipe.Expire(ctx, key, time.Duration(rl.window+1)*time.Second)

	if _, err := pipe.Exec(ctx); err != nil {
		log.Printf("[WARN] RateLimiter: Redis pipeline error: %v - allowing request", err)
		return handler(ctx, req)
	}

	count, err := countCmd.Result()
	if err != nil {
		log.Printf("[WARN] RateLimiter: Redis error: %v - allowing request", err)
		return handler(ctx, req)
	}

	if count > int64(rl.limitPerMin) {
		log.Printf("[WARN] Rate limit exceeded for %s: %d/%d RPM", clientIP, count, rl.limitPerMin)
		return nil, status.Errorf(
			codes.ResourceExhausted,
			"rate limit exceeded: %d requests per minute allowed; retry after %d seconds",
			rl.limitPerMin, rl.window,
		)
	}

	return handler(ctx, req)
}

func (rl *RateLimiter) Close() error {
	if rl.client == nil {
		return nil
	}
	return rl.client.Close()
}
