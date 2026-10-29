package service

import (
	"fmt"
	"log"
	"sync"
	"time"

	"lab1/internal/domain"
	"lab1/internal/entity"
)

// Service - единственный слой бизнес-логики (BookingService):
// хранит все слоты в потокобезопасной map, решает, к какому слоту какой запрос идёт (правила всей системы)

// SlotView — DTO для отдачи наружу (в HTTP JSON и в gRPC-ответ), чтобы
// транспортные слои не тянули наружу внутренние типы entity
type SlotView struct {
	ID        string
	Available bool
}

// BookingService — единственный контракт бизнес-логики фитнес-клуба.
// HTTP-, и gRPC-хендлеры зависят только от этого интерфейса и не знают, что внутри map с мьютексом.
type BookingService interface {
	Book(slotID, client string, from, to time.Time) error
	ListSlots() []SlotView
	GetSlot(slotID string) (SlotView, error)
}

type bookingService struct {
	mu sync.Mutex
	// Полиморфизм: хранится интерфейс Bookable, а не конкретный тип GroupClass/PersonalTraining
	slots map[string]entity.Bookable
}

// NewBookingService — конструктор-фабрика.
func NewBookingService(slots []entity.Bookable) BookingService {
	m := make(map[string]entity.Bookable, len(slots))
	for _, s := range slots {
		m[s.SlotID()] = s
	}
	return &bookingService{slots: m}
}

// Book — записать клиента на занятие или персональную тренировку.
// sync.Mutex защищает map slots от одновременного доступа: HTTP- и
// gRPC-хендлеры работают в разных горутинах (net/http и grpc.Server оба
// обрабатывают запросы конкурентно), и без блокировки конкурентная запись
// в map привела бы к панике рантайма Go
func (s *bookingService) Book(slotID, client string, from, to time.Time) error {
	// defer выполнится при любом выходе из функции, в т.ч. по ранним return с ошибкой
	defer func(start time.Time) {
		log.Printf("Book(slot=%s) processed in %s", slotID, time.Since(start))
	}(time.Now()) //// /??????

	s.mu.Lock()
	defer s.mu.Unlock()

	slot, ok := s.slots[slotID]
	if !ok {
		return fmt.Errorf("%w: slot %s", domain.ErrNotFound, slotID)
	}
	return slot.Book(client, from, to)
}

func (s *bookingService) ListSlots() []SlotView {
	s.mu.Lock()
	defer s.mu.Unlock()

	views := make([]SlotView, 0, len(s.slots))
	now := time.Now()
	for id, slot := range s.slots {
		views = append(views, SlotView{ID: id, Available: slot.IsAvailable(now, now.Add(time.Hour))})
	}
	return views
}

func (s *bookingService) GetSlot(slotID string) (SlotView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	slot, ok := s.slots[slotID]
	if !ok {
		return SlotView{}, fmt.Errorf("%w: slot %s", domain.ErrNotFound, slotID)
	}
	now := time.Now()
	return SlotView{ID: slotID, Available: slot.IsAvailable(now, now.Add(time.Hour))}, nil
}
