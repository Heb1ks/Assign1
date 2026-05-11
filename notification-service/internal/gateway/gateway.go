package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"notification-service/internal/jobqueue"
	"time"
)

// NotificationGateway отправляет уведомления на Mock Gateway сервис
type NotificationGateway struct {
	client  *http.Client
	baseURL string
}

// NewNotificationGateway конструктор
func NewNotificationGateway(gatewayURL string) *NotificationGateway {
	return &NotificationGateway{
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
		baseURL: gatewayURL,
	}
}

// SendNotification отправляет задачу в gateway (POST /notify)
func (ng *NotificationGateway) SendNotification(ctx context.Context, job *jobqueue.Job) error {
	payload := map[string]interface{}{
		"event_type":  job.EventType,
		"occurred_at": job.OccurredAt,
		"id":          job.ID,
		"status":      job.Status,
		"old_status":  job.OldStatus,
		"new_status":  job.NewStatus,
		"data":        job.Data,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	url := fmt.Sprintf("%s/notify", ng.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return fmt.Errorf("request creation error: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := ng.client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusServiceUnavailable {
		return fmt.Errorf("service unavailable")
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return nil
}
