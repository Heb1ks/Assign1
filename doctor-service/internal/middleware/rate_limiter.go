package middleware

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
<<<<<<< HEAD
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
=======
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
>>>>>>> e724c215eb318dd0da60e29464bf279ea24a307b

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
<<<<<<< HEAD
		log.Printf("[WARN] RateLimiter: Redis unavailable: %v — rate limiting disabled", err)
		_ = client.Close()
		return &RateLimiter{client: nil, limitPerMin: limitPerMin, window: 60}
	}

	log.Printf("[INFO] RateLimiter initialized: %d RPM (sliding window)", limitPerMin)
	return &RateLimiter{client: client, limitPerMin: limitPerMin, window: 60}
}

// UnaryServerInterceptor - gRPC-перехватчик, применяется ко всем unary-методам.
=======
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
>>>>>>> e724c215eb318dd0da60e29464bf279ea24a307b
func (rl *RateLimiter) UnaryServerInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
<<<<<<< HEAD
=======
	// Если Redis недоступен, пропускаем rate limiting
>>>>>>> e724c215eb318dd0da60e29464bf279ea24a307b
	if rl.client == nil {
		return handler(ctx, req)
	}

<<<<<<< HEAD
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
=======
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
>>>>>>> e724c215eb318dd0da60e29464bf279ea24a307b
	}

	return handler(ctx, req)
}

<<<<<<< HEAD
=======
// Close graceful shutdown
>>>>>>> e724c215eb318dd0da60e29464bf279ea24a307b
func (rl *RateLimiter) Close() error {
	if rl.client == nil {
		return nil
	}
	return rl.client.Close()
}
