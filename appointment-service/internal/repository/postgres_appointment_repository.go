package repository

import (
	"appointment-service/internal/model"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrNotFound = errors.New("not found")

type PostgresAppointmentRepository struct {
	db *sql.DB
}

func NewPostgresAppointmentRepository(db *sql.DB) *PostgresAppointmentRepository {
	return &PostgresAppointmentRepository{db: db}
}

func (r *PostgresAppointmentRepository) Create(ctx context.Context, a *model.Appointment) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO appointments (id, title, description, doctor_id, status, created_at, updated_at)
         VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		a.ID, a.Title, a.Description, a.DoctorID, string(a.Status), a.CreatedAt, a.UpdatedAt,
	)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("insert appointment: %w", err)
	}
	return tx.Commit()
}

func (r *PostgresAppointmentRepository) GetByID(ctx context.Context, id string) (*model.Appointment, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, title, description, doctor_id, status, created_at, updated_at
         FROM appointments WHERE id = $1`, id,
	)
	a := &model.Appointment{}
	var createdAt, updatedAt time.Time
	err := row.Scan(&a.ID, &a.Title, &a.Description, &a.DoctorID, (*string)(&a.Status), &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get appointment: %w", err)
	}
	a.CreatedAt = createdAt
	a.UpdatedAt = updatedAt
	return a, nil
}

func (r *PostgresAppointmentRepository) GetAll(ctx context.Context) ([]*model.Appointment, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, title, description, doctor_id, status, created_at, updated_at
         FROM appointments ORDER BY created_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("list appointments: %w", err)
	}
	defer rows.Close()

	var list []*model.Appointment
	for rows.Next() {
		a := &model.Appointment{}
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&a.ID, &a.Title, &a.Description, &a.DoctorID, (*string)(&a.Status), &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan appointment: %w", err)
		}
		a.CreatedAt = createdAt
		a.UpdatedAt = updatedAt
		list = append(list, a)
	}
	return list, rows.Err()
}

func (r *PostgresAppointmentRepository) Update(ctx context.Context, a *model.Appointment) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	var exists bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM appointments WHERE id = $1)`, a.ID,
	).Scan(&exists); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("check exists: %w", err)
	}
	if !exists {
		_ = tx.Rollback()
		return ErrNotFound
	}
	_, err = tx.ExecContext(ctx,
		`UPDATE appointments SET status = $1, updated_at = $2 WHERE id = $3`,
		string(a.Status), a.UpdatedAt, a.ID,
	)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("update appointment: %w", err)
	}
	return tx.Commit()
}
