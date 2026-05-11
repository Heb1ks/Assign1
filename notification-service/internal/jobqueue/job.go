package jobqueue

import (
	"time"
)

// Job — задача для обработки в worker pool
type Job struct {
	EventType  string            `json:"event_type"`
	OccurredAt time.Time         `json:"occurred_at"`
	ID         string            `json:"id"`
	Status     string            `json:"status,omitempty"` // для appointments.status_updated
	OldStatus  string            `json:"old_status,omitempty"`
	NewStatus  string            `json:"new_status,omitempty"`
	Data       map[string]string `json:"data,omitempty"` // Дополнительные данные

	// Для отслеживания попыток retry
	RetryCount int
}

// ShouldProcessAppointmentDone — фильтр: обрабатываем только done статусы
func (j *Job) ShouldProcessAppointmentDone() bool {
	return j.EventType == "appointments.status_updated" && j.NewStatus == "done"
}
