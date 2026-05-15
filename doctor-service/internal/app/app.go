package app

import (
	"database/sql"
	"doctor-service/internal/cache"
	"doctor-service/internal/event"
	"doctor-service/internal/middleware" // ← папка лежит в doctor-service/middleware/, НЕ в internal/
	"doctor-service/internal/repository"
	grpchandler "doctor-service/internal/transport/grpc"
	"doctor-service/internal/usecase"
	pb "doctor-service/proto"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
	"google.golang.org/grpc"
)

func Run(addr string) error {
	// ── База данных ──────────────────────────────────────────────────────────
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/doctors?sslmode=disable"
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		return fmt.Errorf("ping db: %w", err)
	}
	log.Println("Doctor Service: database connected")

	if err := runMigrations(db); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// ── NATS (best-effort) ───────────────────────────────────────────────────
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}
	var pub event.EventPublisher
	natsPub, err := event.NewNATSPublisher(natsURL)
	if err != nil {
		log.Printf("[WARN] NATS unavailable (%v); using NoopPublisher", err)
		pub = event.NoopPublisher{}
	} else {
		log.Println("Doctor Service: NATS connected")
		pub = natsPub
	}

	// ── Redis Cache ──────────────────────────────────────────────────────────
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379"
	}
	ttl := 60
	if v := os.Getenv("CACHE_TTL_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			ttl = n
		}
	}
	cacheRepo := cache.NewRedisCacheRepository(redisURL, ttl)
	defer cacheRepo.Close()

	// ── Rate Limiter ─────────────────────────────────────────────────────────
	rpmLimit := 100
	if v := os.Getenv("RATE_LIMIT_RPM"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			rpmLimit = n
		}
	}
	rateLimiter := middleware.NewRateLimiter(redisURL, rpmLimit)
	defer rateLimiter.Close()

	// ── Зависимости ──────────────────────────────────────────────────────────
	repo := repository.NewPostgresDoctorRepository(db)
	uc := usecase.NewDoctorUseCase(repo, cacheRepo, pub)
	srv := grpchandler.NewDoctorServer(uc)

	// ── gRPC сервер ──────────────────────────────────────────────────────────
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	grpcSrv := grpc.NewServer(
		grpc.UnaryInterceptor(rateLimiter.UnaryServerInterceptor),
	)
	pb.RegisterDoctorServiceServer(grpcSrv, srv)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-quit
		log.Println("Doctor Service: shutting down...")
		grpcSrv.GracefulStop()
		pub.Close()
		db.Close()
	}()

	log.Printf("Doctor Service listening on %s", addr)
	return grpcSrv.Serve(lis)
}

func runMigrations(db *sql.DB) error {
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("migration driver: %w", err)
	}
	m, err := migrate.NewWithDatabaseInstance("file://migrations", "postgres", driver)
	if err != nil {
		return fmt.Errorf("migration init: %w", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migration up: %w", err)
	}
	log.Println("Doctor Service: migrations OK")
	return nil
}
