package grpc

import (
	"context"
	"doctor-service/internal/repository"
	"doctor-service/internal/usecase"
	pb "doctor-service/proto"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type DoctorServer struct {
	pb.UnimplementedDoctorServiceServer
	uc usecase.DoctorUseCase
}

func NewDoctorServer(uc usecase.DoctorUseCase) *DoctorServer {
	return &DoctorServer{uc: uc}
}

func (s *DoctorServer) CreateDoctor(ctx context.Context, req *pb.CreateDoctorRequest) (*pb.DoctorResponse, error) {
	if req.FullName == "" {
		return nil, status.Error(codes.InvalidArgument, "full_name is required")
	}
	if req.Email == "" {
		return nil, status.Error(codes.InvalidArgument, "email is required")
	}
	d, err := s.uc.CreateDoctor(ctx, req.FullName, req.Specialization, req.Email)
	if err != nil {
		if errors.Is(err, repository.ErrEmailExists) {
			return nil, status.Error(codes.AlreadyExists, "a doctor with this email already exists")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.DoctorResponse{
		Id: d.ID, FullName: d.FullName,
		Specialization: d.Specialization, Email: d.Email,
	}, nil
}

func (s *DoctorServer) GetDoctor(ctx context.Context, req *pb.GetDoctorRequest) (*pb.DoctorResponse, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	d, err := s.uc.GetDoctorByID(ctx, req.Id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "doctor not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.DoctorResponse{
		Id: d.ID, FullName: d.FullName,
		Specialization: d.Specialization, Email: d.Email,
	}, nil
}

func (s *DoctorServer) ListDoctors(ctx context.Context, _ *pb.ListDoctorsRequest) (*pb.ListDoctorsResponse, error) {
	doctors, err := s.uc.ListDoctors(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	resp := &pb.ListDoctorsResponse{}
	for _, d := range doctors {
		resp.Doctors = append(resp.Doctors, &pb.DoctorResponse{
			Id: d.ID, FullName: d.FullName,
			Specialization: d.Specialization, Email: d.Email,
		})
	}
	return resp, nil
}
