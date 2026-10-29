package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"
)

const (
	// SchemaV1 — версия схемы события (задел на эволюцию контракта).
	SchemaV1 = "v1"
	// TypeBookingCreated — тип события «клиент успешно записан».
	TypeBookingCreated = "booking.created"
)

// Event — доменное событие, публикуемое producer-ом в брокер (JSON).
// Обязательные по ТЗ поля: type, entity_id, occurred_at, schema_version.
// event_id добавлен для идемпотентной обработки на стороне consumer.
type Event struct {
	EventID       string    `json:"event_id"`
	Type          string    `json:"type"`
	EntityID      string    `json:"entity_id"` // идентификатор слота
	OccurredAt    time.Time `json:"occurred_at"`
	SchemaVersion string    `json:"schema_version"`

	Client string    `json:"client"`
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
}

// NewBookingCreated собирает событие v1 о создании записи.
func NewBookingCreated(slotID, client string, from, to time.Time) Event {
	return Event{
		EventID:       newID(),
		Type:          TypeBookingCreated,
		EntityID:      slotID,
		OccurredAt:    time.Now().UTC(),
		SchemaVersion: SchemaV1,
		Client:        client,
		From:          from.UTC(),
		To:            to.UTC(),
	}
}

// Publisher — абстракция над брокером, чтобы сервисный слой не зависел от RabbitMQ.
type Publisher interface {
	Publish(ctx context.Context, e Event) error
	Close() error
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
