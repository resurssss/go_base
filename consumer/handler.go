package main

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Handler — логика consumer: начисление бонусных баллов
// за запись (вторичное действие относительно бронирования).
type Handler struct {
	cfg Config

	// map с баллами клиентов. Воркеров несколько и они работают одновременно,
	// поэтому доступ к map защищён мьютексом: иначе одновременная запись
	// привела бы к панике.
	mu     sync.Mutex
	points map[string]int // клиент -> баллы

	Processed    atomic.Int64 // успешно обработано
	DeadLettered atomic.Int64 // ушло в dead-letter
}

func NewHandler(cfg Config) *Handler {
	return &Handler{cfg: cfg, points: make(map[string]int)}
}

const bonusPerBooking = 10

// Ошибка, которую мы имитируем для демонстрации retry и dead-letter
var errPoison = errors.New("simulated permanent processing failure")

// Handle обрабатывает одно событие. Возвращает nil при успехе, ошибку при неудаче
func (h *Handler) Handle(ev Event) error {
	// нужна, чтобы при демонстрации graceful shutdown
	if h.cfg.ProcessDelay > 0 {
		time.Sleep(h.cfg.ProcessDelay)
	}
	if h.cfg.FailClient != "" && ev.Client == h.cfg.FailClient {
		return fmt.Errorf("client %q: %w", ev.Client, errPoison)
	}

	// Основная работа: прибавляем клиенту 10 баллов
	h.mu.Lock()
	h.points[ev.Client] += bonusPerBooking
	h.mu.Unlock()
	return nil
}

// Points возвращает текущее количество баллов клиента
func (h *Handler) Points(client string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.points[client]
}
