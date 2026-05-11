package model

type Doctor struct {
	ID             string `json:"id"`
	FullName       string `json:"full_name"`
	Specialization string `json:"specialization"`
	Email          string `json:"email"`
}
