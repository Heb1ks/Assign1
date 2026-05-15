#!/bin/bash

set -e

echo " Starting Medical Scheduling Platform (Assignment 4)..."

# Проверка Docker
if ! command -v docker &> /dev/null; then
    echo "Docker not found. Please install Docker."
    exit 1
fi

# Запуск инфраструктуры (PostgreSQL, Redis, NATS)
echo " Starting infrastructure (PostgreSQL, Redis, NATS)..."
docker-compose up -d postgres redis nats

# Ожидание готовности сервисов
echo " Waiting for services to be ready..."
sleep 10

# Создание баз данных
echo "  Creating databases..."
docker exec pg psql -U postgres -c "CREATE DATABASE IF NOT EXISTS doctors;"
docker exec pg psql -U postgres -c "CREATE DATABASE IF NOT EXISTS appointments;"

# Загрузка зависимостей для каждого сервиса
echo " Downloading dependencies..."
cd doctor-service && go mod tidy && cd ..
cd appointment-service && go mod tidy && cd ..
cd notification-service && go mod tidy && cd ..
cd mock-gateway && go mod tidy && cd ..

# Запуск миграций (опционально, если запускать вручную)
echo " Setup complete!"
echo ""
echo " To start services, run in separate terminals:"
echo ""
echo "  Terminal 1 (Doctor Service):"
echo "    cd doctor-service"
echo "    go run ./cmd/doctor-service"
echo ""
echo "  Terminal 2 (Appointment Service):"
echo "    cd appointment-service"
echo "    go run ./cmd/appointment-service"
echo ""
echo "  Terminal 3 (Notification Service):"
echo "    cd notification-service"
echo "    go run ./cmd/notification-service"
echo ""
echo "  Terminal 4 (Mock Gateway):"
echo "    cd mock-gateway"
echo "    go run ./cmd/main.go"
echo ""
echo " To monitor Redis cache:"
echo "    redis-cli MONITOR"
echo ""