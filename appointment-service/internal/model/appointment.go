package model

import "time"

type Status string

const (
	StatusNew        Status = "new"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
)

// IsValid - доменный метод=
func (s Status) IsValid() bool {
	switch s {
	case StatusNew, StatusInProgress, StatusDone:
		return true
	}
	return false
}

type Appointment struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	DoctorID    string    `json:"doctor_id"`
	Status      Status    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
