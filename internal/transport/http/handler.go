package httptransport

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"lab1/internal/domain"
	"lab1/internal/service"
)

// Отвечает за транспортировку запросов

type Handler struct {
	svc service.BookingService
}

func NewHandler(svc service.BookingService) *Handler {
	return &Handler{svc: svc}
}

// Routes собирает все эндпоинты в http.ServeMux
func (h *Handler) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.handleHealth)          // GET  /health
	mux.HandleFunc("/slots", h.handleSlots)            // GET  /slots
	mux.HandleFunc("/bookings", h.handleCreateBooking) // POST /bookings
	return mux
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (h *Handler) handleSlots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	views := h.svc.ListSlots()
	writeJSON(w, http.StatusOK, views)
}

type createBookingRequest struct {
	SlotID string    `json:"slot_id"`
	Client string    `json:"client"`
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
}

func (h *Handler) handleCreateBooking(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req createBookingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body: "+err.Error(), http.StatusBadRequest)
		return
	}

	err := h.svc.Book(req.SlotID, req.Client, req.From, req.To)
	if err == nil {
		writeJSON(w, http.StatusCreated, map[string]string{"status": "booked"})
		return
	}

	// Сопоставление доменных ошибок с HTTP-кодами через errors.Is/errors.As
	// errors.Is ищет конкретное значение ошибки
	// errors.As ищет ошибку конкретного типа
	var conflictErr *domain.SlotConflictError
	switch {
	case errors.Is(err, domain.ErrValidation):
		http.Error(w, err.Error(), http.StatusBadRequest) // 400
	case errors.Is(err, domain.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound) // 404
	case errors.As(err, &conflictErr):
		http.Error(w, conflictErr.Error(), http.StatusConflict) // 409
	default:
		http.Error(w, "internal error", http.StatusInternalServerError) // 500
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
