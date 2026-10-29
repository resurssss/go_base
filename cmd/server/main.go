package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"google.golang.org/grpc"
	healthsrv "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"lab1/internal/domain"
	"lab1/internal/entity"
	"lab1/internal/events"
	"lab1/internal/service"
	grpctransport "lab1/internal/transport/grpc"
	httptransport "lab1/internal/transport/http"
	pb "lab1/pb"
)

func main() {
	httpPort := getEnv("HTTP_PORT", "8080")
	grpcPort := getEnv("GRPC_PORT", "9090")
	// SHUTDOWN_TIMEOUT_SECONDS — не бизнес-логика, а инфраструктурная настройка
	// (лаба 2, п.3 ТЗ: таймауты приходят из ConfigMap, а не хардкодятся).
	shutdownTimeout := getEnvDuration("SHUTDOWN_TIMEOUT_SECONDS", 5*time.Second)

	svc := service.NewBookingService(seedSlots())

	// ---------- Лаба 3: публикация доменных событий в RabbitMQ ----------
	// Если RABBITMQ_URL не задан, сервис работает как в лабе 2 (без событий).
	if url := getEnv("RABBITMQ_URL", ""); url != "" {
		pub := events.NewRabbitPublisher(url)
		defer pub.Close()
		svc = service.NewPublishingService(svc, pub, 2*time.Second)
	} else {
		log.Println("RABBITMQ_URL not set: event publishing disabled")
	}

	// ---------- HTTP ----------
	httpHandler := httptransport.NewHandler(svc)
	httpServer := &http.Server{
		Addr:    ":" + httpPort,
		Handler: httpHandler.Routes(),
	}

	// ---------- gRPC ----------
	grpcServer := grpc.NewServer()
	pb.RegisterBookingServiceServer(grpcServer, grpctransport.NewServer(svc))

	// Стандартный grpc_health_v1, отдельно от бизнес-RPC (требование п.7 ТЗ лабы 1).
	health := healthsrv.NewServer()
	healthpb.RegisterHealthServer(grpcServer, health)
	health.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	// Server reflection — упрощает ручное тестирование grpcurl/evans и,
	// заодно, не мешает нативной gRPC readiness-пробе Kubernetes 1.24+.
	reflection.Register(grpcServer)

	lis, err := net.Listen("tcp", ":"+grpcPort)
	if err != nil {
		log.Fatalf("failed to listen on grpc port %s: %v", grpcPort, err)
	}

	go func() {
		log.Printf("http server listening on :%s", httpPort)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server error: %v", err)
		}
	}()

	go func() {
		log.Printf("grpc server listening on :%s", grpcPort)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("grpc server error: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	log.Println("shutdown signal received, stopping servers...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("http graceful shutdown error: %v", err)
	}
	grpcServer.GracefulStop()

	log.Println("shutdown complete")
}

func seedSlots() []entity.Bookable {
	classPrice, err := domain.NewMoney(1000, "USD")
	if err != nil {
		log.Fatalf("seed: %v", err)
	}
	personalPrice, err := domain.NewMoney(4000, "USD")
	if err != nil {
		log.Fatalf("seed: %v", err)
	}

	yogaStart := time.Date(2025, 6, 2, 18, 0, 0, 0, time.UTC)
	yoga, err := entity.NewGroupClass("class-yoga", "Yoga", "Elena", yogaStart, yogaStart.Add(time.Hour), 15, classPrice)
	if err != nil {
		log.Fatalf("seed: %v", err)
	}

	zumbaStart := time.Date(2025, 6, 2, 19, 30, 0, 0, time.UTC)
	zumba, err := entity.NewGroupClass("class-zumba", "Zumba", "Dmitry", zumbaStart, zumbaStart.Add(45*time.Minute), 20, classPrice)
	if err != nil {
		log.Fatalf("seed: %v", err)
	}

	personal1, err := entity.NewPersonalTraining("personal-1", "Sergey", personalPrice, 30)
	if err != nil {
		log.Fatalf("seed: %v", err)
	}

	return []entity.Bookable{yoga, zumba, personal1}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	seconds, err := strconv.Atoi(v)
	if err != nil || seconds <= 0 {
		log.Printf("invalid %s=%q, using default %s", key, v, fallback)
		return fallback
	}
	return time.Duration(seconds) * time.Second
}
