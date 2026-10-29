package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Топология брокера. Объявляется идемпотентно и producer-ом, и consumer-ом
// (с одинаковыми аргументами), чтобы события не терялись, если producer
// стартовал раньше consumer-а (без привязанной очереди сообщение было бы отброшено).
const (
	ExchangeName   = "fitness.events"
	DLXName        = "fitness.events.dlx"
	QueueName      = "booking.created.queue"
	DeadQueueName  = "booking.created.dead"
	RoutingKey     = TypeBookingCreated
	DeadRoutingKey = "booking.created.dead"
)

// DeclareTopology объявляет exchange, очередь, dead-letter exchange и dead-letter очередь.
func DeclareTopology(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(ExchangeName, "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange: %w", err)
	}
	if err := ch.ExchangeDeclare(DLXName, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dlx: %w", err)
	}
	if _, err := ch.QueueDeclare(DeadQueueName, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dead queue: %w", err)
	}
	if err := ch.QueueBind(DeadQueueName, DeadRoutingKey, DLXName, false, nil); err != nil {
		return fmt.Errorf("bind dead queue: %w", err)
	}
	args := amqp.Table{
		"x-dead-letter-exchange":    DLXName,
		"x-dead-letter-routing-key": DeadRoutingKey,
	}
	if _, err := ch.QueueDeclare(QueueName, true, false, false, false, args); err != nil {
		return fmt.Errorf("declare queue: %w", err)
	}
	if err := ch.QueueBind(QueueName, RoutingKey, ExchangeName, false, nil); err != nil {
		return fmt.Errorf("bind queue: %w", err)
	}
	return nil
}

// RabbitPublisher — публикация с publisher confirms и ленивым (пере)подключением.
// Если брокер недоступен при старте producer, сервис всё равно работает (деградация),
// а подключение повторяется при следующей публикации.
type RabbitPublisher struct {
	url string

	mu   sync.Mutex
	conn *amqp.Connection
	ch   *amqp.Channel
}

func NewRabbitPublisher(url string) *RabbitPublisher {
	return &RabbitPublisher{url: url}
}

func (p *RabbitPublisher) connect() error {
	conn, err := amqp.DialConfig(p.url, amqp.Config{Dial: amqp.DefaultDial(2 * time.Second)})
	if err != nil {
		return fmt.Errorf("dial rabbitmq: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("open channel: %w", err)
	}
	if err := DeclareTopology(ch); err != nil {
		_ = conn.Close()
		return err
	}
	if err := ch.Confirm(false); err != nil {
		_ = conn.Close()
		return fmt.Errorf("enable confirms: %w", err)
	}
	p.conn, p.ch = conn, ch
	log.Println("rabbitmq publisher connected")
	return nil
}

func (p *RabbitPublisher) reset() {
	if p.conn != nil {
		_ = p.conn.Close()
	}
	p.conn, p.ch = nil, nil
}

// Publish отправляет событие и ждёт подтверждения брокера, но не дольше дедлайна ctx.
func (p *RabbitPublisher) Publish(ctx context.Context, e Event) error {
	body, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ch == nil || p.ch.IsClosed() {
		p.reset()
		if err := p.connect(); err != nil {
			return err
		}
	}

	conf, err := p.ch.PublishWithDeferredConfirmWithContext(ctx, ExchangeName, e.Type, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		MessageId:    e.EventID,
		Type:         e.Type,
		Timestamp:    e.OccurredAt,
		Body:         body,
	})
	if err != nil {
		p.reset()
		return fmt.Errorf("publish: %w", err)
	}
	acked, err := conf.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("wait confirm: %w", err)
	}
	if !acked {
		return errors.New("broker nacked the message")
	}
	return nil
}

func (p *RabbitPublisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn != nil && !p.conn.IsClosed() {
		return p.conn.Close()
	}
	return nil
}
