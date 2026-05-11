package main

import (
	"appointment-service/internal/app"
	"log"
	"os"
)

func main() {
	addr := os.Getenv("GRPC_ADDR")
	if addr == "" {
		addr = ":50052"
	}
	if err := app.Run(addr); err != nil {
		log.Fatalf("Appointment Service failed: %v", err)
	}
}
