package repository

import (
	"context"
	"database/sql"
	"doctor-service/internal/model"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

const pgErrUniqueViolation = "23505"

var ErrNotFound = errors.New("not found")
var ErrEmailExists = errors.New("email already exists")

type PostgresDoctorRepository struct {
	db *sql.DB
}

func NewPostgresDoctorRepository(db *sql.DB) *PostgresDoctorRepository {
	return &PostgresDoctorRepository{db: db}
}

func (r *PostgresDoctorRepository) Create(ctx context.Context, d *model.Doctor) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO doctors (id, full_name, specialization, email) VALUES ($1, $2, $3, $4)`,
		d.ID, d.FullName, d.Specialization, d.Email,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgErrUniqueViolation {
			return ErrEmailExists
		}
		return fmt.Errorf("create doctor: %w", err)
	}
	return nil
}

func (r *PostgresDoctorRepository) GetByID(ctx context.Context, id string) (*model.Doctor, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, full_name, specialization, email FROM doctors WHERE id = $1`, id,
	)
	d := &model.Doctor{}
	if err := row.Scan(&d.ID, &d.FullName, &d.Specialization, &d.Email); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get doctor: %w", err)
	}
	return d, nil
}

func (r *PostgresDoctorRepository) GetAll(ctx context.Context) ([]*model.Doctor, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, full_name, specialization, email FROM doctors ORDER BY created_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("list doctors: %w", err)
	}
	defer rows.Close()

	var doctors []*model.Doctor
	for rows.Next() {
		d := &model.Doctor{}
		if err := rows.Scan(&d.ID, &d.FullName, &d.Specialization, &d.Email); err != nil {
			return nil, fmt.Errorf("scan doctor: %w", err)
		}
		doctors = append(doctors, d)
	}
	return doctors, rows.Err()
}

func (r *PostgresDoctorRepository) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM doctors WHERE email = $1)`, email,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("exists by email: %w", err)
	}
	return exists, nil
}
