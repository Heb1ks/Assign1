FROM golang:1.25.3-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o service ./cmd/doctor-service

FROM alpine:latest
COPY --from=builder /app/service .
COPY --from=builder /app/migrations ./migrations   
ENTRYPOINT ["./service"]