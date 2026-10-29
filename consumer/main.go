package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const consumerTag = "fitness-bonus-consumer"

func main() {
	cfg := LoadConfig()
	log.Printf("consumer starting: workers=%d buffer=%d max_attempts=%d shutdown_timeout=%s",
		cfg.Workers, cfg.BufferSize, cfg.MaxAttempts, cfg.ShutdownTimeout)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-sigs
		log.Printf("signal %s received, stopping reading new messages...", s)
		cancel()
	}()

	// Подключение к RabbitMQ
	conn, err := dialWithRetry(ctx, cfg.RabbitURL, 30, 2*time.Second)
	if err != nil {
		log.Fatalf("cannot connect to rabbitmq: %v", err)
	}
	defer conn.Close()

	// Через канал отправляются все команды брокеру
	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("open channel: %v", err)
	}

	// Создаём очереди и обменники, если их ещё нет
	if err := declareTopology(ch); err != nil {
		log.Fatalf("topology: %v", err)
	}
	// Prefetch = число воркеров: брокер не выдаёт больше неподтверждённых
	// сообщений, чем мы способны обрабатывать параллельно.
	if err := ch.Qos(cfg.Workers, 0, false); err != nil {
		log.Fatalf("qos: %v", err)
	}
	// autoAck=false: ack только после успешной обработки.
	msgs, err := ch.Consume(queueName, consumerTag, false, false, false, false, nil)
	if err != nil {
		log.Fatalf("consume: %v", err)
	}

	// Запуск конвейера
	handler := NewHandler(cfg)

	if err := startControlListener(ctx, conn, handler); err != nil {
		log.Fatalf("control listener: %v", err)
	}
	// Буферизованный канал между читателем и воркерами
	events := make(chan Event, cfg.BufferSize)
	// Запускаем воркеров
	wg := startWorkers(cfg.Workers, events, handler, cfg)

	// Запускаем читателя в отдельной горутине
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		readLoop(ctx, msgs, events)
	}()

	// Ждём сигнала либо неожиданной остановки читателя (например, обрыв соединения).
	select {
	case <-ctx.Done():
	case <-readerDone:
		log.Println("reader stopped unexpectedly (broker connection lost?)")
		cancel()
	}

	// Graceful shutdown
	// Просим брокер не присылать новые сообщения; уже выданные unacked остаются за нами.
	if err := ch.Cancel(consumerTag, false); err != nil {
		log.Printf("cancel consumer: %v", err)
	}
	<-readerDone // reader закрыл events → воркеры дочитают канал и выйдут

	// Ждём воркеров, но не дольше ShutdownTimeout
	workersDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(workersDone)
	}()

	select {
	case <-workersDone:
		log.Println("all workers finished")
	case <-time.After(cfg.ShutdownTimeout):
		log.Printf("shutdown timeout %s exceeded: unfinished messages stay unacked and will be redelivered", cfg.ShutdownTimeout)
	}

	log.Printf("shutdown complete: processed=%d dead_lettered=%d",
		handler.Processed.Load(), handler.DeadLettered.Load())
}

// readLoop — отдельная горутина-читатель: очередь → внутренний канал.
// По отмене ctx прекращает чтение и закрывает канал events.
// превращает JSON в Event и кладёт в канал out для воркеров
// Завершается при отмене ctx или при закрытии msgs, и закрывает канал out
func readLoop(ctx context.Context, msgs <-chan amqp.Delivery, out chan<- Event) {
	defer close(out)
	for {
		select {
		case <-ctx.Done():
			return
		case d, ok := <-msgs:
			if !ok {
				return
			}
			// Разбираем JSON в структуру Event
			var ev Event
			if err := json.Unmarshal(d.Body, &ev); err != nil || ev.EventID == "" || ev.SchemaVersion != "v1" {
				// «Ядовитое» сообщение: битый JSON, нет id или неизвестная версия схемы
				log.Printf("invalid message (err=%v), sending to dead-letter", err)
				_ = d.Nack(false, false)
				continue
			}
			ev.Delivery = d

			// Передаём событие воркерам. Если канал заполнен, ждём места,
			// но если в это время пришла остановка, не зависаем.
			select {
			case out <- ev:
			case <-ctx.Done():
				// Не успели передать воркерам — возвращаем в очередь.
				_ = d.Nack(false, true)
				return
			}
		}
	}
}

// dialWithRetry подключается к RabbitMQ, повторяя попытки
// Нужна потому, что при docker compose up RabbitMQ может ещё не быть готов принимать подключения.
func dialWithRetry(ctx context.Context, url string, attempts int, delay time.Duration) (*amqp.Connection, error) {
	var lastErr error
	for i := 1; i <= attempts; i++ {
		conn, err := amqp.Dial(url)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		log.Printf("rabbitmq not ready (%d/%d): %v", i, attempts, err)
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("interrupted: %w", ctx.Err())
		case <-time.After(delay):
		}
	}
	return nil, lastErr
}
