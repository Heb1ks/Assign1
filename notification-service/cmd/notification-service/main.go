package main

import (
	"log"
	"notification-service/internal/subscriber"
	"os"
	"os/signal"
	"strconv"
	"syscall"
)

func main() {
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}

	// WORKER_POOL_SIZE читаем из env, дефолт — 3 (по заданию).
	poolSize := 3
	if v := os.Getenv("WORKER_POOL_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			poolSize = n
		}
	}

	sub, err := subscriber.New(natsURL, poolSize)
	if err != nil {
		log.Fatalf("Notification Service: %v", err)
	}
	if err := sub.Subscribe(); err != nil {
		log.Fatalf("Notification Service: subscribe: %v", err)
	}

	log.Println("Notification Service: waiting for events (Ctrl+C to stop)")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit

	sub.Drain()
}
