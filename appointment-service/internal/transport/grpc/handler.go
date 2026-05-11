package grpc

import (
	"appointment-service/internal/model"
	"appointment-service/internal/usecase"
	pb "appointment-service/proto"
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AppointmentServer struct {
	pb.UnimplementedAppointmentServiceServer
	uc usecase.AppointmentUseCase
}

func NewAppointmentServer(uc usecase.AppointmentUseCase) *AppointmentServer {
	return &AppointmentServer{uc: uc}
}

func (s *AppointmentServer) CreateAppointment(ctx context.Context, req *pb.CreateAppointmentRequest) (*pb.AppointmentResponse, error) {
	if req.Title == "" {
		return nil, status.Error(codes.InvalidArgument, "title is required")
	}
	if req.DoctorId == "" {
		return nil, status.Error(codes.InvalidArgument, "doctor_id is required")
	}

	a, err := s.uc.CreateAppointment(ctx, req.Title, req.Description, req.DoctorId)
	if err != nil {
		return nil, mapAppointmentError(err)
	}
	return appointmentToProto(a), nil
}

func (s *AppointmentServer) GetAppointment(ctx context.Context, req *pb.GetAppointmentRequest) (*pb.AppointmentResponse, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	a, err := s.uc.GetAppointmentByID(ctx, req.Id)
	if err != nil {
		return nil, status.Error(codes.NotFound, "appointment not found")
	}
	return appointmentToProto(a), nil
}

func (s *AppointmentServer) ListAppointments(ctx context.Context, _ *pb.ListAppointmentsRequest) (*pb.ListAppointmentsResponse, error) {
	list, err := s.uc.ListAppointments(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	resp := &pb.ListAppointmentsResponse{}
	for _, a := range list {
		resp.Appointments = append(resp.Appointments, appointmentToProto(a))
	}
	return resp, nil
}

func (s *AppointmentServer) UpdateAppointmentStatus(ctx context.Context, req *pb.UpdateStatusRequest) (*pb.AppointmentResponse, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if req.Status == "" {
		return nil, status.Error(codes.InvalidArgument, "status is required")
	}

	a, err := s.uc.UpdateStatus(ctx, req.Id, model.Status(req.Status))
	if err != nil {
		return nil, mapAppointmentError(err)
	}
	return appointmentToProto(a), nil
}

func appointmentToProto(a *model.Appointment) *pb.AppointmentResponse {
	return &pb.AppointmentResponse{
		Id:          a.ID,
		Title:       a.Title,
		Description: a.Description,
		DoctorId:    a.DoctorID,
		Status:      string(a.Status),
		CreatedAt:   a.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:   a.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

func mapAppointmentError(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "unavailable"):
		return status.Error(codes.Unavailable, msg)
	case strings.Contains(msg, "does not exist"):
		return status.Error(codes.FailedPrecondition, msg)
	case strings.Contains(msg, "not found"):
		return status.Error(codes.NotFound, msg)
	case strings.Contains(msg, "forbidden"), strings.Contains(msg, "invalid status"):
		return status.Error(codes.InvalidArgument, msg)
	default:
		return status.Error(codes.Internal, msg)
	}
}
