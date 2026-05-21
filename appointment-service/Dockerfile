FROM golang:1.25.3-alpine AS builder
WORKDIR /app

COPY doctor-service/ ./doctor-service/

COPY appointment-service/go.mod appointment-service/go.sum ./appointment-service/
WORKDIR /app/appointment-service
RUN go mod download

COPY appointment-service/ .
RUN go build -o service ./cmd/appointment-service

FROM alpine:latest
COPY --from=builder /app/appointment-service/service .
COPY --from=builder /app/appointment-service/migrations ./migrations  
ENTRYPOINT ["./service"]