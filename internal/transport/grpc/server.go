// этот файл разбирает proto-запрос, вызывает
// service-слой и мапит доменные ошибки в grpc/codes — прямой аналог того,
// что internal/transport/http/handler.go делает с HTTP-статусами

package grpctransport

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"lab1/internal/domain"
	"lab1/internal/service"
	pb "lab1/pb"
)

type Server struct {
	// Встраивание Unimplemented*Server — стандартная практика grpc-go для
	// forward-совместимости: если в будущем в .proto добавят метод,
	// Server продолжит удовлетворять интерфейсу за счёт "заглушки" из
	// сгенерированного кода.
	pb.UnimplementedBookingServiceServer
	svc service.BookingService
}

func NewServer(svc service.BookingService) *Server {
	return &Server{svc: svc}
}

func (s *Server) CreateBooking(ctx context.Context, req *pb.CreateBookingRequest) (*pb.CreateBookingResponse, error) {
	err := s.svc.Book(req.GetSlotId(), req.GetClient(), req.GetFrom().AsTime(), req.GetTo().AsTime())
	if err == nil {
		return &pb.CreateBookingResponse{Status: "booked"}, nil
	}

	//   errors.Is ErrValidation -> HTTP 400  <-> codes.InvalidArgument
	//   errors.Is ErrNotFound   -> HTTP 404  <-> codes.NotFound
	//   errors.As ConflictError -> HTTP 409  <-> codes.AlreadyExists
	//   иначе                    -> HTTP 500  <-> codes.Internal
	var conflictErr *domain.SlotConflictError
	switch {
	case errors.Is(err, domain.ErrValidation):
		return nil, status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, domain.ErrNotFound):
		return nil, status.Error(codes.NotFound, err.Error())
	case errors.As(err, &conflictErr):
		return nil, status.Error(codes.AlreadyExists, conflictErr.Error())
	default:
		return nil, status.Error(codes.Internal, "internal error")
	}
}

func (s *Server) ListSlots(ctx context.Context, req *pb.ListSlotsRequest) (*pb.ListSlotsResponse, error) {
	views := s.svc.ListSlots()
	resp := &pb.ListSlotsResponse{Slots: make([]*pb.SlotView, 0, len(views))}
	for _, v := range views {
		resp.Slots = append(resp.Slots, &pb.SlotView{Id: v.ID, Available: v.Available})
	}
	return resp, nil
}
