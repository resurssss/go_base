package main

import (
	"context"
	"fmt"
	"log"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Обменник для служебных команд всем экземплярам consumer.
const controlExchange = "fitness.control"

// ResetCache очищает кэш этого экземпляра (таблицу баллов в памяти).
// Метод объявлен здесь, а не в handler.go, чтобы не менять существующий файл.
func (h *Handler) ResetCache() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := len(h.points)
	h.points = make(map[string]int)
	return n
}

// startControlListener подписывает этот экземпляр на команды управления.
// Тип обменника fanout: каждое сообщение копируется во ВСЕ привязанные очереди.
// У каждого экземпляра своя очередь (имя генерирует брокер), поэтому
// команду получают все поды, а не один из них.
func startControlListener(ctx context.Context, conn *amqp.Connection, h *Handler) error {
	// Отдельный канал, чтобы не смешивать со Qos и ack основного потока.
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("control channel: %w", err)
	}
	if err := ch.ExchangeDeclare(controlExchange, "fanout", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare control exchange: %w", err)
	}

	// Очередь без имени ("") = брокер придумает уникальное.
	// exclusive=true: доступна только этому подключению;
	// autoDelete=true: исчезнет вместе с подключением (при остановке пода).
	q, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		return fmt.Errorf("declare control queue: %w", err)
	}
	if err := ch.QueueBind(q.Name, "", controlExchange, false, nil); err != nil {
		return fmt.Errorf("bind control queue: %w", err)
	}

	// autoAck=true: команда одноразовая, повторная доставка не нужна.
	msgs, err := ch.Consume(q.Name, "", true, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume control: %w", err)
	}

	go func() {
		defer ch.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case m, ok := <-msgs:
				if !ok {
					return
				}
				if string(m.Body) == "flush" {
					n := h.ResetCache()
					log.Printf("control: cache flushed (had %d clients)", n)
				}
			}
		}
	}()
	return nil
}
