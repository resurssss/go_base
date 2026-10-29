package main

import (
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Названия обменников, очередей и ключей маршрутизации
// Топология должна совпадать с producer (internal/events/rabbit.go).
const (
	exchangeName   = "fitness.events"
	dlxName        = "fitness.events.dlx"
	queueName      = "booking.created.queue"
	deadQueueName  = "booking.created.dead"
	routingKey     = "booking.created"
	deadRoutingKey = "booking.created.dead"
)

// declareTopology создаёт в RabbitMQ все нужные обменники и очереди
// если объект уже существует с теми же параметрами,
// ничего не происходит
func declareTopology(ch *amqp.Channel) error {
	// topic (маршрутизация по ключу)
	// durable=true (переживёт перезапуск брокера)
	if err := ch.ExchangeDeclare(exchangeName, "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange: %w", err)
	}
	// Dead-letter обменник (тип direct: маршрут строго по ключу)
	if err := ch.ExchangeDeclare(dlxName, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dlx: %w", err)
	}
	// Очередь «брака» и её привязка к dead-letter обменнику
	if _, err := ch.QueueDeclare(deadQueueName, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dead queue: %w", err)
	}
	if err := ch.QueueBind(deadQueueName, deadRoutingKey, dlxName, false, nil); err != nil {
		return fmt.Errorf("bind dead queue: %w", err)
	}
	// Основная очередь с настройкой dead-letter:
	// если сообщение отклонено (Nack без возврата в очередь), брокер сам
	// перекладывает его в обменник dlxName с ключом deadRoutingKey,
	// то есть в очередь deadQueueName. Ничего не теряется.
	args := amqp.Table{
		"x-dead-letter-exchange":    dlxName,
		"x-dead-letter-routing-key": deadRoutingKey,
	}
	if _, err := ch.QueueDeclare(queueName, true, false, false, false, args); err != nil {
		return fmt.Errorf("declare queue: %w", err)
	}
	// Привязываем основную очередь к основному обменнику:
	// события с ключом "booking.created" будут попадать в эту очередь.
	if err := ch.QueueBind(queueName, routingKey, exchangeName, false, nil); err != nil {
		return fmt.Errorf("bind queue: %w", err)
	}
	return nil
}
