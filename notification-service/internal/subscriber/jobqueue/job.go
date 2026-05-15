package jobqueue

import (
	"context"
	"time"
)

// Job — задача для фоновой обработки.
type Job struct {
	EventType     string            `json:"event_type"`
	OccurredAt    time.Time         `json:"occurred_at"`
	ID            string            `json:"id"`
	AppointmentID string            `json:"appointment_id"`
	DoctorID      string            `json:"doctor_id"`
	OldStatus     string            `json:"old_status,omitempty"`
	NewStatus     string            `json:"new_status,omitempty"`
	Status        string            `json:"status,omitempty"`
	Data          map[string]string `json:"data,omitempty"`
	RetryCount    int               `json:"-"`
}

// ShouldProcessAppointmentDone — true только для appointments.status_updated с new_status=done.
func (j *Job) ShouldProcessAppointmentDone() bool {
	return j.EventType == "appointments.status_updated" && j.NewStatus == "done"
}

// Notifier — интерфейс для отправки уведомления во внешний шлюз.
// Определён здесь чтобы избежать циклической зависимости между jobqueue и gateway.
type Notifier interface {
	SendNotification(ctx context.Context, job *Job) error
}
