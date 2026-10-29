package main

import (
	"log"
	"sync"
	"time"
)

// startWorkers запускает фиксированное число воркеров, читающих из канала events.
// Воркеры завершаются, когда канал закрыт и опустошён — так при остановке
// уже взятые из очереди сообщения дообрабатываются.
func startWorkers(n int, events <-chan Event, h *Handler, cfg Config) *sync.WaitGroup {
	var wg sync.WaitGroup
	for i := 1; i <= n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for ev := range events {
				process(id, ev, h, cfg)
			}
			log.Printf("worker %d: channel drained, exiting", id)
		}(i)
	}
	return &wg
}

// process обрабатывает одно событие: до MaxAttempts попыток с нарастающей паузой,
// а если все попытки неудачны, отправляет сообщение в dead-letter
func process(workerID int, ev Event, h *Handler, cfg Config) {
	var lastErr error
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		lastErr = h.Handle(ev)
		if lastErr == nil {
			// Успех: сообщаем брокеру, что сообщение обработано (Ack)
			// После этого RabbitMQ удаляет его из очереди
			if err := ev.Delivery.Ack(false); err != nil {
				log.Printf("worker %d: ack failed event_id=%s: %v", workerID, ev.EventID, err)
				return
			}
			total := h.Processed.Add(1)
			log.Printf("worker %d: processed event_id=%s client=%s total=%d", workerID, ev.EventID, ev.Client, total)
			return
		}
		log.Printf("worker %d: attempt %d/%d failed event_id=%s: %v", workerID, attempt, cfg.MaxAttempts, ev.EventID, lastErr)
		if attempt < cfg.MaxAttempts {
			time.Sleep(cfg.BackoffBase << (attempt - 1)) // 200ms, 400ms, 800ms...
		}
	}

	// Попытки исчерпаны: Nack без requeue → брокер перекладывает сообщение в DLX → dead queue.
	if err := ev.Delivery.Nack(false, false); err != nil {
		log.Printf("worker %d: nack failed event_id=%s: %v", workerID, ev.EventID, err)
		return
	}
	dead := h.DeadLettered.Add(1)
	log.Printf("worker %d: event_id=%s sent to dead-letter after %d attempts (dead total=%d): %v",
		workerID, ev.EventID, cfg.MaxAttempts, dead, lastErr)
}
