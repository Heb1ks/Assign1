FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o service ./cmd/appointment-service

FROM alpine:latest
COPY --from=builder /app/service .
ENTRYPOINT ["./service"]