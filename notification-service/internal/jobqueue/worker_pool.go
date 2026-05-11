package jobqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"notification-service/internal/gateway"
	"notification-service/internal/logger"
	"os"
	"sync"
	"time"
)

// WorkerPool управляет pool воркеров для обработки задач
type WorkerPool struct {
	jobsChan   chan *Job
	numWorkers int
	logger     *logger.JSONLogger
	gateway    *gateway.NotificationGateway
	cache      CacheRepository // интерфейс для проверки идемпотентности
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
}

// CacheRepository интерфейс для хранилища (Redis)
type CacheRepository interface {
	CheckIdempotencyKey(ctx context.Context, key string) (bool, error)
	SetIdempotencyKey(ctx context.Context, key string, ttlSeconds int) error
}

// NewWorkerPool конструктор
func NewWorkerPool(
	numWorkers int,
	logger *logger.JSONLogger,
	gateway *gateway.NotificationGateway,
	cache CacheRepository,
) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())
	return &WorkerPool{
		jobsChan:   make(chan *Job, 100), // Буфер на 100 задач
		numWorkers: numWorkers,
		logger:     logger,
		gateway:    gateway,
		cache:      cache,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Start запускает pool воркеров
func (wp *WorkerPool) Start() {
	for i := 0; i < wp.numWorkers; i++ {
		wp.wg.Add(1)
		go wp.worker(i)
	}
	log.Printf("[INFO] Worker pool started: %d workers", wp.numWorkers)
}

// Submit добавляет задачу в очередь
func (wp *WorkerPool) Submit(job *Job) {
	select {
	case wp.jobsChan <- job:
		// OK
	case <-wp.ctx.Done():
		log.Println("[WARN] Worker pool is shutting down, job rejected")
	}
}

// worker выполняет задачи из канала
func (wp *WorkerPool) worker(id int) {
	defer wp.wg.Done()

	for {
		select {
		case job := <-wp.jobsChan:
			if job == nil {
				return
			}
			wp.processJob(job)

		case <-wp.ctx.Done():
			return
		}
	}
}

// processJob выполняет одну задачу с retry логикой
func (wp *WorkerPool) processJob(job *Job) {
	// Шаг 1: Проверяем идемпотентность
	idempotencyKey := GenerateIdempotencyKey(job.EventType, job.ID, job.OccurredAt)

	exists, err := wp.cache.CheckIdempotencyKey(wp.ctx, idempotencyKey)
	if err != nil {
		wp.logger.Warn(fmt.Sprintf("Failed to check idempotency key: %v", err))
	}

	if exists {
		wp.logger.Info("Event already processed (idempotency check)", map[string]string{
			"event_type": job.EventType,
			"id":         job.ID,
			"key":        idempotencyKey,
		})
		return
	}

	// Шаг 2: Фильтруем только appointments.status_updated с new_status == "done"
	if !job.ShouldProcessAppointmentDone() {
		wp.logger.Info("Event filtered (not appointment done)", map[string]string{
			"event_type": job.EventType,
			"id":         job.ID,
		})
		return
	}

	// Шаг 3: Retry логика с экспоненциальным бэк-оффом
	maxRetries := 3
	backoffSeconds := []int{1, 2, 4}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		err := wp.gateway.SendNotification(wp.ctx, job)

		if err == nil {
			// Успех! Сохраняем идемпотентность ключ
			if err := wp.cache.SetIdempotencyKey(wp.ctx, idempotencyKey, IdempotencyTTL); err != nil {
				wp.logger.Error("Failed to save idempotency key", err)
			}

			wp.logger.Info("Notification sent successfully", map[string]string{
				"event_type": job.EventType,
				"id":         job.ID,
				"attempt":    fmt.Sprintf("%d", attempt+1),
			})
			return
		}

		lastErr = err
		job.RetryCount = attempt + 1

		// Если это не ошибка 503, не переpыбуем
		if !isServiceUnavailable(err) {
			wp.logger.Error(fmt.Sprintf("Non-retryable error: %v", err), err)
			wp.writeDeadLetterLog(job, err)
			return
		}

		// Если это последняя попытка, пишем Dead Letter Log
		if attempt == maxRetries {
			wp.logger.Error(fmt.Sprintf("Max retries exceeded (%d)", maxRetries), lastErr)
			wp.writeDeadLetterLog(job, lastErr)
			return
		}

		// Экспоненциальный бэк-офф с jitter
		sleepDuration := time.Duration(backoffSeconds[attempt]) * time.Second
		jitter := time.Duration(rand.Intn(1000)) * time.Millisecond
		totalSleep := sleepDuration + jitter

		wp.logger.Warn(fmt.Sprintf("Retry %d/%d after %v (error: %v)", attempt+1, maxRetries, totalSleep, err))
		select {
		case <-time.After(totalSleep):
		case <-wp.ctx.Done():
			return
		}
	}
}

// writeDeadLetterLog пишет JSON в stderr (Dead Letter)
func (wp *WorkerPool) writeDeadLetterLog(job *Job, err error) {
	deadLetterEntry := map[string]interface{}{
		"time":        time.Now().RFC3339,
		"type":        "dead_letter",
		"event_type":  job.EventType,
		"id":          job.ID,
		"retry_count": job.RetryCount,
		"error":       err.Error(),
		"occurred_at": job.OccurredAt,
		"status":      job.Status,
		"old_status":  job.OldStatus,
		"new_status":  job.NewStatus,
	}

	wp.logger.Error(fmt.Sprintf("Dead letter: %s/%s", job.EventType, job.ID), err)

	// Дополнительный JSON в stderr для визуализации
	data, _ := json.Marshal(deadLetterEntry)
	fmt.Fprintf(os.Stderr, "%s\n", string(data))
}

// isServiceUnavailable проверяет, является ли ошибка 503
func isServiceUnavailable(err error) bool {
	if err == nil {
		return false
	}
	return err.Error() == "service unavailable" || err.Error() == "HTTP 503"
}

// Stop graceful shutdown
func (wp *WorkerPool) Stop() error {
	wp.cancel()
	close(wp.jobsChan)
	wp.wg.Wait()
	log.Println("[INFO] Worker pool stopped")
	return nil
}
