package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"notification-service/internal/subscriber/jobqueue" // ← правильный путь
	"time"
)

// NotificationGateway реализует jobqueue.Notifier и отправляет POST /notify
// на Mock Notification Gateway.
type NotificationGateway struct {
	client  *http.Client
	baseURL string
}

func NewNotificationGateway(gatewayURL string) *NotificationGateway {
	return &NotificationGateway{
		client:  &http.Client{Timeout: 5 * time.Second},
		baseURL: gatewayURL,
	}
}

// SendNotification отправляет задачу в шлюз.
// Возвращает "service unavailable" при HTTP 503 (retry-able),
// nil при HTTP 200.
func (ng *NotificationGateway) SendNotification(ctx context.Context, job *jobqueue.Job) error {
	idempotencyKey := jobqueue.GenerateIdempotencyKey(job.EventType, job.ID, job.OccurredAt)

	payload := map[string]interface{}{
		"idempotency_key": idempotencyKey,
		"channel":         "email",
		"recipient":       "patient@clinic.kz",
		"message": fmt.Sprintf(
			"Your appointment %s with doctor %s is complete.",
			job.AppointmentID, job.DoctorID,
		),
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	url := fmt.Sprintf("%s/notify", ng.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return fmt.Errorf("request creation error: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ng.client.Do(req)
	if err != nil {
		return fmt.Errorf("service unavailable")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusServiceUnavailable {
		return fmt.Errorf("service unavailable")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return nil
}
