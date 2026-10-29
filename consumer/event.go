package main

import (
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Event — структура, в которую превращается JSON-сообщение из очереди
// Теги `json:"..."` говорят, какое поле JSON соответствует какому полю структуры.
type Event struct {
	EventID       string    `json:"event_id"`
	Type          string    `json:"type"`
	EntityID      string    `json:"entity_id"`
	OccurredAt    time.Time `json:"occurred_at"`
	SchemaVersion string    `json:"schema_version"`

	Client string    `json:"client"`
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`

	// Delivery — «квитанция» этого сообщения в RabbitMQ.
	// Через неё воркер сообщает брокеру: обработано (Ack) или не получилось (Nack).
	// Тег json:"-" означает, что поле в JSON не участвует: оно заполняется кодом, а не приходит из сети.
	Delivery amqp.Delivery `json:"-"`
}
