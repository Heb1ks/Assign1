package jobqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"notification-service/internal/subscriber/logger" // ← правильный путь
	"os"
	"sync"
	"time"
)

// WorkerPool управляет пулом горутин-воркеров.
type WorkerPool struct {
	jobsChan   chan *Job
	numWorkers int
	logger     *logger.JSONLogger
	notifier   Notifier // ← интерфейс, не конкретный тип
	cache      CacheRepository
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
}

func NewWorkerPool(
	numWorkers int,
	l *logger.JSONLogger,
	notifier Notifier,
	cache CacheRepository,
) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())
	return &WorkerPool{
		jobsChan:   make(chan *Job, 100),
		numWorkers: numWorkers,
		logger:     l,
		notifier:   notifier,
		cache:      cache,
		ctx:        ctx,
		cancel:     cancel,
	}
}

func (wp *WorkerPool) Start() {
	for i := 0; i < wp.numWorkers; i++ {
		wp.wg.Add(1)
		go wp.worker(i)
	}
	log.Printf("[INFO] Worker pool started: %d workers", wp.numWorkers)
}

func (wp *WorkerPool) Submit(job *Job) {
	select {
	case wp.jobsChan <- job:
	case <-wp.ctx.Done():
		log.Println("[WARN] Worker pool shutting down, job rejected")
	}
}

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

func (wp *WorkerPool) processJob(job *Job) {
	// Шаг 1: фильтр — обрабатываем только appointments.status_updated + done.
	if !job.ShouldProcessAppointmentDone() {
		return
	}

	idempotencyKey := GenerateIdempotencyKey(job.EventType, job.ID, job.OccurredAt)

	// Шаг 2: проверяем идемпотентность.
	exists, err := wp.cache.CheckIdempotencyKey(wp.ctx, idempotencyKey)
	if err != nil {
		wp.logger.Warn(fmt.Sprintf("idempotency check failed: %v", err))
	}
	if exists {
		wp.logger.Info("duplicate job dropped (already processed)", map[string]string{
			"job_id": idempotencyKey,
			"id":     job.ID,
		})
		return
	}

	wp.logger.JobLog(logger.LogEntry{
		Level:  "info",
		JobID:  idempotencyKey,
		Status: "enqueued",
	})

	// Шаг 3: retry с exponential backoff (1s, 2s, 4s).
	backoffs := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}
	maxRetries := len(backoffs)

	var lastErr error
	for attempt := 1; attempt <= maxRetries+1; attempt++ {
		wp.logger.JobLog(logger.LogEntry{
			Level:   "info",
			JobID:   idempotencyKey,
			Attempt: attempt,
			Status:  "processing",
		})

		sendErr := wp.notifier.SendNotification(wp.ctx, job)
		if sendErr == nil {
			// Успех: помечаем ключ как "done" в Redis (TTL 24ч).
			if cerr := wp.cache.SetIdempotencyKey(wp.ctx, idempotencyKey, IdempotencyTTL); cerr != nil {
				wp.logger.Warn(fmt.Sprintf("failed to save idempotency key: %v", cerr))
			}
			wp.logger.JobLog(logger.LogEntry{
				Level:   "info",
				JobID:   idempotencyKey,
				Attempt: attempt,
				Status:  "success",
			})
			return
		}

		lastErr = sendErr
		if !isTransient(sendErr) {
			wp.writeDeadLetter(idempotencyKey, job, attempt, lastErr)
			return
		}

		if attempt > maxRetries {
			break
		}

		sleep := backoffs[attempt-1] + time.Duration(rand.Intn(500))*time.Millisecond
		wp.logger.JobLog(logger.LogEntry{
			Level:   "warn",
			JobID:   idempotencyKey,
			Attempt: attempt,
			Status:  "retry",
			Error:   sendErr.Error(),
		})
		select {
		case <-time.After(sleep):
		case <-wp.ctx.Done():
			return
		}
	}

	wp.writeDeadLetter(idempotencyKey, job, maxRetries, lastErr)
}

func (wp *WorkerPool) writeDeadLetter(jobID string, job *Job, attempt int, err error) {
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	entry := map[string]interface{}{
		"time":    time.Now().Format(time.RFC3339),
		"level":   "error",
		"job_id":  jobID,
		"attempt": attempt,
		"status":  "dead_letter",
		"error":   errStr,
	}
	data, _ := json.Marshal(entry)
	fmt.Fprintf(os.Stderr, "%s\n", string(data))

	wp.logger.JobLog(logger.LogEntry{
		Level:   "error",
		JobID:   jobID,
		Attempt: attempt,
		Status:  "dead_letter",
		Error:   errStr,
	})
}

func isTransient(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return msg == "service unavailable" || msg == "HTTP 503"
}

func (wp *WorkerPool) Stop() error {
	// Cancel context first — workers will exit via ctx.Done().
	// Only close the channel AFTER all workers have stopped to avoid
	// a "send on closed channel" panic in Submit().
	wp.cancel()
	wp.wg.Wait()
	close(wp.jobsChan)
	log.Println("[INFO] Worker pool stopped")
	return nil
}
