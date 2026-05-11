package main

import (
	"log"
	"notification-service/internal/subscriber"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}

	sub, err := subscriber.New(natsURL, 6)
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
