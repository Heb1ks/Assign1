package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// seenKeys хранит уже обработанные idempotency_key в памяти.
// В production здесь был бы Redis или БД; для симуляции достаточно map + mutex.
var (
	seenKeys   = make(map[string]struct{})
	seenKeysMu sync.Mutex
)

func main() {
	port := os.Getenv("GATEWAY_PORT")
	if port == "" {
		port = "8080"
	}
	addr := fmt.Sprintf(":%s", port)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /notify", handleNotify)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	})

	log.Printf("[INFO] Mock Gateway starting on %s", addr)

	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
		<-quit
		log.Println("[INFO] Shutting down Mock Gateway...")
		_ = server.Close()
	}()

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[ERROR] Server error: %v", err)
	}
	log.Println("[INFO] Mock Gateway stopped")
}

// handleNotify обрабатывает POST /notify.
// - 20% вероятность HTTP 503 (симуляция transient failure).
// - Повторный idempotency_key → 200 {"status":"duplicate"}.
// - Новый ключ → 200 {"status":"accepted"}.
func handleNotify(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		IdempotencyKey string `json:"idempotency_key"`
		Channel        string `json:"channel"`
		Recipient      string `json:"recipient"`
		Message        string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		logRequest("error", payload.IdempotencyKey, r.RemoteAddr, 400)
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid json"}`)
		return
	}

	// 20% случаев — симулируем недоступность сервиса.
	if rand.Intn(100) < 20 {
		logRequest("503_simulated", payload.IdempotencyKey, r.RemoteAddr, 503)
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error":"service unavailable"}`)
		return
	}

	// Проверяем идемпотентность.
	seenKeysMu.Lock()
	_, alreadySeen := seenKeys[payload.IdempotencyKey]
	if !alreadySeen {
		seenKeys[payload.IdempotencyKey] = struct{}{}
	}
	seenKeysMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if alreadySeen {
		logRequest("duplicate", payload.IdempotencyKey, r.RemoteAddr, 200)
		fmt.Fprint(w, `{"status":"duplicate"}`)
		return
	}

	logRequest("accepted", payload.IdempotencyKey, r.RemoteAddr, 200)
	fmt.Fprint(w, `{"status":"accepted"}`)
}

// logRequest пишет структурированный JSON в stdout.
func logRequest(result, key, remoteAddr string, code int) {
	entry := map[string]interface{}{
		"time":            time.Now().Format(time.RFC3339),
		"result":          result,
		"idempotency_key": key,
		"remote_addr":     remoteAddr,
		"status_code":     code,
	}
	data, _ := json.Marshal(entry)
	log.Println(string(data))
}
