package client

import "context"

type DoctorClient interface {
	DoctorExists(ctx context.Context, doctorID string) (bool, error)
}
