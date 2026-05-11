package app

import (
	"appointment-service/internal/client"
	"appointment-service/internal/event"
	"appointment-service/internal/repository"
	grpchandler "appointment-service/internal/transport/grpc"
	"appointment-service/internal/usecase"
	pb "appointment-service/proto"
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
	"google.golang.org/grpc"
)

func Run(addr string) error {
	// db
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/appointments?sslmode=disable"
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		return fmt.Errorf("ping db: %w", err)
	}
	log.Println("Appointment Service: database connected")

	//  migration
	if err := runMigrations(db); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	//  nats (best-effort)
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}
	var pub event.EventPublisher
	natsPub, err := event.NewNATSPublisher(natsURL)
	if err != nil {
		log.Printf("WARN: cannot connect to NATS (%v); using NoopPublisher", err)
		pub = event.NoopPublisher{}
	} else {
		log.Println("Appointment Service: NATS connected")
		pub = natsPub
	}

	//  Doctor Service grpc client
	doctorAddr := os.Getenv("DOCTOR_SERVICE_ADDR")
	if doctorAddr == "" {
		doctorAddr = "localhost:50051"
	}
	dc, err := client.NewGRPCDoctorClient(doctorAddr)
	if err != nil {
		return fmt.Errorf("doctor client: %w", err)
	}

	// dependecis
	repo := repository.NewPostgresAppointmentRepository(db)
	uc := usecase.NewAppointmentUseCase(repo, dc, pub)
	srv := grpchandler.NewAppointmentServer(uc)

	// grpc server
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	grpcSrv := grpc.NewServer()
	pb.RegisterAppointmentServiceServer(grpcSrv, srv)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-quit
		log.Println("Appointment Service: shutting down...")
		grpcSrv.GracefulStop()
		pub.Close()
		db.Close()
	}()

	log.Printf("Appointment Service listening on %s (Doctor: %s)", addr, doctorAddr)
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
	log.Println("Appointment Service: migrations OK")
	return nil
}