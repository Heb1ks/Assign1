package main

import (
	"doctor-service/internal/app"
	"log"
	"os"
)

func main() {
	addr := os.Getenv("GRPC_ADDR")
	if addr == "" {
		addr = ":50051"
	}
	if err := app.Run(addr); err != nil {
		log.Fatalf("Doctor Service failed: %v", err)
	}
}
