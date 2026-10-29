# Лабораторная работа №3. Асинхронное взаимодействие: очереди сообщений и конкурентность в Go

## Архитектура

```
 HTTP/gRPC ──► producer (fitness-booking) ──publish──► RabbitMQ ──consume──► consumer (бонусные баллы)
                 │ 1. Book() — сохранить            exchange fitness.events (topic)
                 │ 2. publish booking.created       queue    booking.created.queue
                 │                                  └─(reject/nack)─► fitness.events.dlx ─► booking.created.dead
```

Два независимых процесса: producer (`go.mod` `lab1`) и consumer (`consumer/go.mod`), связь только через брокер.
У consumer нет HTTP/gRPC API. Его предметная логика — начисление 10 бонусных баллов клиенту за каждую запись.

Что добавлено (существующие файлы лаб 1-2 не менялись, кроме нескольких строк в `main.go`):

| Файл | Назначение |
|---|---|
| `internal/events/event.go` | контракт события v1, интерфейс `Publisher` |
| `internal/events/rabbit.go` | RabbitMQ-публикатор (confirms, ленивый реконнект), топология очередей |
| `internal/service/publishing_service.go` | декоратор `BookingService`: Book → затем публикация |
| `consumer/*` | отдельный сервис: читатель, worker pool, retry, dead-letter |
| `docker-compose.override.yml` | RabbitMQ + consumer, `RABBITMQ_URL` для producer |
| `scripts/load.sh` | нагрузочная проверка (`bash scripts/load.sh 100`) |

## Выбор брокера: RabbitMQ

- dead-letter exchange/queue — встроенная возможность (аргументы очереди + `Nack(requeue=false)`), для NATS нужен JetStream и ручная настройка;
- ручные подтверждения (ack/nack) и redelivery при падении consumer — ровно то, что требует ТЗ;
- publisher confirms позволяют producer узнать, что брокер принял сообщение;
- удобный веб-интерфейс для демонстрации очередей и DLQ.

## Формат события (JSON)

```json
{
  "event_id": "9f1c...",
  "type": "booking.created",
  "entity_id": "personal-1",
  "occurred_at": "2026-01-01T10:00:00Z",
  "schema_version": "v1",
  "client": "Anna",
  "from": "...", "to": "..."
}
```

Обязательные поля по ТЗ: `type`, `entity_id`, `occurred_at`, `schema_version`. `event_id` добавлен для идемпотентности.

## Публикация после сохранения состояния

`publishingService.Book` сначала вызывает исходный `Book` (состояние сохранено), и только при `nil` публикует событие.

**Что не так с публикацией до коммита.** Если опубликовать событие, а затем сохранение упадёт (конфликт слота, валидация, паника, перезапуск пода), то consumer начислит баллы за запись, которой не существует. Это «фантомное» событие: оно уже у других сервисов и его невозможно отозвать без компенсирующих действий. Обратная ситуация (сохранили, но не опубликовали) безопаснее: она приводит к потере уведомления, а не к ложному факту.

**Ошибки публикации.** Публикация идёт с таймаутом 2 с (`context.WithTimeout`). При ошибке или таймауте запись не откатывается, в лог пишется `DEGRADED: ...`, клиент получает успех. Остаётся проблема dual write: между сохранением и публикацией возможна потеря события (например, падение процесса). Решается паттерном Transactional Outbox, который выходит за рамки этой лабы (тема Saga, лекция 4).

## Consumer: горутины и каналы

```
RabbitMQ ─► readLoop (1 горутина) ─► chan Event (буфер) ─► worker 1..N (WaitGroup)
```

- `readLoop` — отдельная горутина, читает `amqp.Delivery`, парсит JSON и кладёт `Event` в канал.
- Worker pool: `WORKERS` горутин (по умолчанию 4), читают из канала параллельно.
- `Qos(prefetch = WORKERS)` — брокер не выдаёт больше неподтверждённых сообщений, чем можно обработать.
- Ack только после успешной обработки (`autoAck=false`).

### Graceful shutdown

1. SIGINT/SIGTERM → `cancel()` контекста (`context.WithCancel` + `os/signal`).
2. `ch.Cancel(consumerTag)` — брокер перестаёт слать новые сообщения; `readLoop` выходит и **закрывает канал `events`**.
3. Воркеры доделывают всё, что уже лежит в канале и в обработке (цикл `for ev := range events`), затем выходят.
4. `main` ждёт `wg.Wait()` не дольше `SHUTDOWN_TIMEOUT_SECONDS`. Если не уложились, необработанные сообщения остаются неподтверждёнными, и брокер вернёт их в очередь при закрытии соединения (ничего не теряется).

## Устойчивость к сбоям

- Ошибка обработки → до `MAX_ATTEMPTS` попыток (3), между ними `time.Sleep` с удвоением: 200 мс, 400 мс, ...
- Попытки исчерпаны → `Nack(requeue=false)`, и брокер по `x-dead-letter-exchange` перекладывает сообщение в `booking.created.dead`. Оно не теряется.
- Битый JSON / неизвестная версия схемы → сразу в dead-letter (повторы бессмысленны).

## Дубликаты и неупорядоченная доставка (описание, без реализации)

RabbitMQ гарантирует **at-least-once**: если consumer обработал сообщение, но упал до `ack` (или ack потерялся по сети), брокер доставит его повторно. Аналогично при ретраях/реконнектах producer. Итог: **дублирующаяся доставка**. Бонусы за одну запись начислятся дважды.

Out-of-order: при нескольких воркерах и повторной доставке события, относящиеся к одной сущности, могут обрабатываться не в порядке создания. Например, `booking.cancelled` придёт раньше `booking.created` (если бы такое событие появилось).

Поэтому обработчик должен быть **идемпотентным**: повторная обработка того же события даёт тот же результат, что и однократная. Для этого сохраняют `event_id` обработанных событий (таблица dedup с уникальным ключом, обновление баллов в одной транзакции) либо делают операции по природе идемпотентными (установка значения, а не инкремент). Для порядка проверяют версию/`occurred_at` сущности и игнорируют устаревшее событие. Это та же идея, на которой строятся шаги Saga и компенсации.

## Запуск

Недостающие файлы лаб 1-2 (`internal/domain/`, `pb/`, `proto/`) уже должны лежать в проекте.

```bash
go mod tidy
(cd consumer && go mod tidy)
docker build -t fitness-booking-service:local .
docker compose up --build
```

RabbitMQ UI: http://localhost:15672 (guest / guest).

## Демонстрация

**1. Обмен через брокер**
```bash
curl -X POST localhost:8080/bookings -H 'Content-Type: application/json' \
  -d '{"slot_id":"personal-1","client":"Anna","from":"2026-02-01T10:00:00Z","to":"2026-02-01T11:00:00Z"}'
docker compose logs fitness-booking | grep "event published"
docker compose logs consumer | grep processed
```

**2. Нагрузка: ни одно событие не теряется**

отправил 100 параллельных запросов на запись.
```bash
bash scripts/load.sh 100
```
Ожидаем: `100 201` (все запросы успешны). Затем считаем обработанные события:
```bash
docker compose logs consumer | grep -c 'processed event_id'
```
команда посчитала, сколько раз consumer написал в лог processed event_id.
Ожидаем: `100`.

**3. Graceful shutdown**
В `docker-compose.override.yml` поставить `PROCESS_DELAY_MS: "500"`, пересоздать consumer, запустить `bash scripts/load.sh 50` и сразу:
```bash
docker compose stop consumer
docker compose logs consumer | tail -30
```
В логе: `signal terminated received` → `worker N: channel drained` → `shutdown complete: processed=X`. Остаток сообщений
(если не успели) остаётся в очереди; после `docker compose start consumer` они дообрабатываются, сумма = 50.

**4. Retry и dead-letter**
```bash
curl -X POST localhost:8080/bookings -H 'Content-Type: application/json' \
  -d '{"slot_id":"personal-1","client":"poison","from":"2026-03-01T10:00:00Z","to":"2026-03-01T11:00:00Z"}'
docker compose logs consumer | grep -E 'attempt|dead-letter'
```
В логе три попытки с растущей паузой и `sent to dead-letter`; в UI RabbitMQ в очереди `booking.created.dead` появится 1 сообщение.

## Ответы на вопросы устного опроса

- **Почему публикация после сохранения?** См. раздел выше: иначе возможны «фантомные» события о том, чего не произошло.
- **Что при падении consumer в момент обработки без ack?** Соединение рвётся, брокер видит неподтверждённые сообщения и возвращает их в очередь (`redelivered=true`), они уйдут другому/перезапущенному consumer. Потери нет, но возможны дубликаты, поэтому нужна идемпотентность.