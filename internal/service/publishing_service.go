package service

import (
	"context"
	"log"
	"time"

	"lab1/internal/events"
)

// publishingService — декоратор над BookingService (лаба 3).
// Не меняет бизнес-логику: сначала делегирует в исходный сервис (локальное
// сохранение состояния), и только при успехе публикует доменное событие.
// ListSlots и GetSlot проходят «насквозь» через встраивание интерфейса.
type publishingService struct {
	BookingService
	pub     events.Publisher
	timeout time.Duration
}

func NewPublishingService(inner BookingService, pub events.Publisher, timeout time.Duration) BookingService {
	return &publishingService{BookingService: inner, pub: pub, timeout: timeout}
}

func (s *publishingService) Book(slotID, client string, from, to time.Time) error {
	// 1. Сначала локальное сохранение. Ошибка — события нет вообще.
	if err := s.BookingService.Book(slotID, client, from, to); err != nil {
		return err
	}

	// 2. Состояние уже сохранено → публикуем. Таймаут ограничивает задержку ответа клиенту.
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	ev := events.NewBookingCreated(slotID, client, from, to)
	if err := s.pub.Publish(ctx, ev); err != nil {
		// Деградация: запись остаётся, откат не делаем, клиенту — успех.
		log.Printf("DEGRADED: booking saved but event %s not published (slot=%s client=%s): %v",
			ev.EventID, slotID, client, err)
		return nil
	}
	log.Printf("event published: id=%s type=%s slot=%s", ev.EventID, ev.Type, slotID)
	return nil
}
