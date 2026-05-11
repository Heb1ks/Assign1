package client

import (
	"context"
	doctorpb "doctor-service/proto"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// GRPCDoctorClient wraps the generated gRPC stub behind the DoctorClient interface.
type GRPCDoctorClient struct {
	stub doctorpb.DoctorServiceClient
}

func NewGRPCDoctorClient(target string) (*GRPCDoctorClient, error) {
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("cannot connect to Doctor Service at %s: %w", target, err)
	}
	return &GRPCDoctorClient{stub: doctorpb.NewDoctorServiceClient(conn)}, nil
}

// DoctorExists calls DoctorService.GetDoctor and translates gRPC status codes
// into the two sentinel outcomes the use case needs.
func (c *GRPCDoctorClient) DoctorExists(ctx context.Context, doctorID string) (bool, error) {
	_, err := c.stub.GetDoctor(ctx, &doctorpb.GetDoctorRequest{Id: doctorID})
	if err == nil {
		return true, nil
	}
	st, _ := status.FromError(err)
	switch st.Code() {
	case codes.NotFound:
		return false, nil // doctor simply doesn't exist
	case codes.Unavailable:
		return false, fmt.Errorf("doctor service unavailable: %s", st.Message())
	default:
		return false, fmt.Errorf("doctor service error (%s): %s", st.Code(), st.Message())
	}
}
