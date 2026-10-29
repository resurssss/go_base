# Лаба №2 — контейнеризация и оркестрация (Docker + Kubernetes)

Бизнес-логика не менялась — все файлы из лабы 1
(`internal/`, `cmd/`, `proto/`) остались как есть. Добавлены только:
`Dockerfile`, `Dockerfile.singlestage` (для сравнения размеров),
`docker-compose.yml`, `.dockerignore`, `k8s/*.yaml`.

---

## Шаг 0. Что нужно поставить

- Docker Desktop (или Docker Engine + docker-compose plugin)
- `kind` **или** `minikube` (в примерах ниже — `kind`, для minikube отличия
  отмечены отдельно)
- `kubectl`

Проверка:
```bash
docker version
kind version      # или: minikube version
kubectl version --client
```

## Шаг 1. Подготовить pb/ (как в лабе 1)

Docker-сборка требует уже сгенерированный gRPC-код — `protoc` внутри
Docker-сборки не запускается, только `go build`. Поэтому сгенерируйте
`pb/` **до** `docker build`, как в лабе 1, и закоммитьте в репозиторий:

```bash
go mod tidy
mkdir -p pb
protoc \
  --go_out=pb --go_opt=paths=source_relative \
  --go-grpc_out=pb --go-grpc_opt=paths=source_relative \
  --proto_path=proto \
  proto/booking.proto
```

## Шаг 2. Собрать multi-stage образ и сравнить размер (п.1 ТЗ)

```bash
docker build -t fitness-booking-service:local -f Dockerfile .
docker build -t fitness-booking-service:singlestage -f Dockerfile.singlestage .

docker images | grep fitness-booking-service
```

Ожидаемая картина (порядок величин, у вас цифры будут немного другими):

| Образ | Примерный размер | Почему |
|---|---|---|
| `singlestage` | ~900 MB – 1.1 GB | внутри весь `golang:1.22` — компилятор, кеш модулей, исходники |
| `local` (multi-stage) | ~15–25 MB | только `alpine` + один статический бинарник |

**Почему это важно не только "для эстетики":**
- Время `docker pull` при деплое/масштабировании — с образом в 1 ГБ
  каждый новый под в Kubernetes будет стартовать заметно дольше, особенно
  при холодном старте ноды или при частых rolling update.
- Площадь атаки — в однослойном образе лежит весь Go-тулчейн и (потенциально)
  `go.sum`-кеш с исходниками зависимостей; в multi-stage — вообще нечем
  скомпрометироваться сверх самого бинарника.
- Место в container registry и на нодах кластера — при большом количестве
  сервисов/версий разница в порядки ощутима на реальных счетах за облако.

Занесите фактические цифры `docker images` в отчёт.

## Шаг 3. docker-compose (п.2 ТЗ)

```bash
docker compose up --build
```

Что должно произойти:
1. Сначала стартует `postgres`, `docker compose` ждёт, пока его
   `healthcheck` (`pg_isready`) не станет успешным.
2. Только после этого стартует `fitness-booking` — именно за это отвечает
   `depends_on.postgres.condition: service_healthy` в `docker-compose.yml`,
   а не просто порядок объявления сервисов.
3. У `fitness-booking` тоже есть свой `healthcheck` (`wget --spider` на
   `/health`) — в выводе `docker compose ps` через некоторое время статус
   должен смениться на `healthy`.

Проверка в отдельном терминале:
```bash
docker compose ps
curl -i http://localhost:8080/health
```

Postgres в этой лабе сервисом реально не используется (лаба 1 хранит всё
in-memory, и это менять нельзя) — задача этого шага показать сам механизм
многосервисного compose с healthcheck-зависимостью, задел под лабу 3/
курсовую.

Остановить:
```bash
docker compose down -v
```

## Шаг 4. Поднять локальный кластер и загрузить образ

### kind
```bash
kind create cluster --name fitness-lab2
kind load docker-image fitness-booking-service:local --name fitness-lab2
```

### minikube (альтернатива)
```bash
minikube start
minikube image load fitness-booking-service:local
```

Оба манифеста (`k8s/deployment.yaml`) используют
`imagePullPolicy: Never` — это осознанно: образ уже локально загружен в
кластер командой выше, и Kubernetes не должен пытаться тянуть его из
внешнего registry (которого для `fitness-booking-service:local` попросту
не существует).

## Шаг 5. Применить манифесты (п.3 ТЗ)

```bash
kubectl apply -f k8s/configmap.yaml
kubectl apply -f k8s/deployment.yaml
kubectl apply -f k8s/service.yaml

kubectl get pods -w
```

Дождитесь `STATUS: Running` и `READY: 1/1` у обеих реплик (`Ctrl+C`, когда
оба пода готовы). Если под долго не становится `Ready` — смотрите
`kubectl describe pod <имя>` и `kubectl logs <имя>`.

Проверить сервис изнутри кластера:
```bash
kubectl run tmp-curl --rm -it --image=curlimages/curl --restart=Never -- \
  curl -i http://fitness-booking:8080/health
```

## Шаг 6. Обоснование выбора проб (устный опрос, п.3/4 ТЗ)

В `k8s/deployment.yaml` уже есть комментарии прямо на месте, коротко ещё
раз здесь:

- **liveness → HTTP `/health`.** Liveness отвечает на вопрос «процесс вообще
  жив, или его нужно перезапустить?». Это должна быть максимально дешёвая,
  простая проверка, минимально зависящая от состояния бизнес-логики — GET
  без тела, без парсинга protobuf, без establishment gRPC-соединения.
  Если liveness ошибётся и решит, что живой под мёртв — kubelet его убьёт
  и пересоздаст, поэтому проверка должна быть надёжной и лёгкой.
- **readiness → gRPC, нативная проба Kubernetes 1.24+
  (`readinessProbe.grpc.port`).** Readiness отвечает на другой вопрос: «под
  жив, но готов ли он ПРЯМО СЕЙЧАС принимать трафик?». Мы намеренно
  проверяем именно gRPC-транспорт (`grpc_health_v1`, уже зарегистрированный
  в `main.go` лабы 1) — так убеждаемся, что оба транспорта (HTTP и gRPC)
  реально поднялись и слушают, а не только HTTP. Если бы readiness тоже
  ходил в HTTP `/health`, поломка исключительно gRPC-сервера (например, он
  не успел забиндиться на порт) не была бы обнаружена, и под остался бы в
  `Service`, отдавая ошибки по gRPC.
- Выбрана именно **нативная** gRPC-проба (`grpc:` в манифесте), а не
  `exec`-обёртка через `grpc-health-probe`, чтобы не тащить в образ лишний
  бинарник — начиная с Kubernetes 1.24 (alpha) / 1.27 (GA) kubelet умеет
  ходить по gRPC health-протоколу напрямую, без sidecar. **Если ваш
  локальный кластер младше 1.24** (проверить: `kubectl version`), нативная
  проба работать не будет — тогда нужно либо обновить `kind`/`minikube` до
  свежего образа кластера, либо перейти на `exec`-вариант (см. ниже,
  "Альтернатива для старых кластеров").

### Альтернатива для старых кластеров (< 1.24)

Если нативная gRPC-проба недоступна, замените в `k8s/deployment.yaml`:

```yaml
readinessProbe:
  grpc:
    port: 9090
```

на:

```yaml
readinessProbe:
  exec:
    command: ["/grpc_health_probe", "-addr=:9090"]
```

и добавьте в `Dockerfile` (в стадию runtime) загрузку бинарника
`grpc-health-probe`:

```dockerfile
FROM golang:1.22 AS grpc_health_probe
RUN GRPC_HEALTH_PROBE_VERSION=v0.4.24 && \
    wget -qO /bin/grpc_health_probe \
    https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/${GRPC_HEALTH_PROBE_VERSION}/grpc_health_probe-linux-amd64 && \
    chmod +x /bin/grpc_health_probe
```
и `COPY --from=grpc_health_probe /bin/grpc_health_probe /grpc_health_probe`
в финальную стадию. В отчёте зафиксируйте, какой вариант выбрали и почему
(версия вашего кластера).

## Шаг 7. Демонстрация сценариев (п.4 ТЗ, нужно минимум 2)

### Сценарий A — `kubectl delete pod`: Deployment пересоздаёт под без простоя

```bash
kubectl get pods -o wide
# запомните имя одного из подов, например fitness-booking-7d9...-abcde

kubectl delete pod <имя-пода>

# сразу после этого, в отдельном терминале, в цикле:
while true; do
  kubectl run tmp-curl --rm -i --image=curlimages/curl --restart=Never -- \
    curl -s -o /dev/null -w "%{http_code}\n" http://fitness-booking:8080/health
  sleep 0.5
done
```

Ожидаемое поведение: `kubectl get pods -w` покажет, что удалённый под
пропал и почти сразу появился новый (`ContainerCreating` → `Running` →
`Ready`) — это делает `Deployment`, а не вы руками. При этом трафик через
`Service` не прерывается: пока новый под ещё не `Ready`, `Service`
продолжает слать запросы на **оставшуюся** (вторую) реплику — именно
поэтому `replicas: 2`, а не 1. Сохраните в отчёт вывод `kubectl get pods -w`
и лог кода ответов (не должно быть провалов в `000`/`5xx`, кроме, может
быть, единичного запроса ровно в момент удаления, если такой попал на
удаляемый под ДО того, как трафик успел переключиться — это тоже стоит
зафиксировать и объяснить в отчёте).

Для сравнения — сделайте то же самое с `docker compose`: там сервис один
(`replicas` в compose нет), поэтому `docker compose restart fitness-booking`
приводит к полному простою на время перезапуска — Kubernetes же эту
проблему решает за счёт нескольких реплик и Service, который сам исключает
неготовые поды.

### Сценарий B — «зависший» под: readiness исключает его из Service без падения

Так как менять бизнес-логику (эндпоинт `/health`) запрещено ТЗ, добиться
эффекта «сервис жив, но не готов» нужно **не трогая код**, а через
конфигурацию — это даже более честная демонстрация того, что решает именно
readinessProbe, а не приложение:

```bash
# Сломать порт, на который слушает приложение, не трогая объявленный
# containerPort (readiness/Service продолжают стучаться в порт 9090,
# а приложение реально переезжает на другой — connection refused).
kubectl patch configmap fitness-booking-config \
  --type merge -p '{"data":{"GRPC_PORT":"19090"}}'

kubectl rollout restart deployment fitness-booking
kubectl get pods -w
```

Ожидаемое поведение: новые поды переходят в `Running`, но **не переходят в
`Ready`** (`READY: 0/1`) — readinessProbe на gRPC-порт 9090 получает
`connection refused`, потому что сервис реально слушает 19090.
`kubectl describe pod <имя>` в Events покажет `Readiness probe failed`.
Пока `rollingUpdate.maxUnavailable: 0`, старые (ещё исправные) поды
Kubernetes не станет убивать, пока новые не подтвердят готовность — но так
как новые никогда не станут `Ready`, деплой зависнет (это тоже стоит
показать в отчёте — Kubernetes не подставит под удар исправные поды из-за
плохого рулаута). Восстановление:

```bash
kubectl patch configmap fitness-booking-config \
  --type merge -p '{"data":{"GRPC_PORT":"9090"}}'
kubectl rollout restart deployment fitness-booking
kubectl rollout status deployment fitness-booking
```

Ключевой момент для отчёта: пока поды были `NotReady`, `Service`
`fitness-booking` **не направлял на них трафик** (проверяется через
`kubectl get endpoints fitness-booking` — адреса неготовых подов исчезают
из списка) — то есть readinessProbe сработал как задумано: не убил под
(в отличие от liveness), а просто временно вывел его из-под трафика.

### Сценарий C (опционально, для полноты) — rolling update без простоя

```bash
docker build -t fitness-booking-service:v2 -f Dockerfile .
kind load docker-image fitness-booking-service:v2 --name fitness-lab2
kubectl set image deployment/fitness-booking fitness-booking=fitness-booking-service:v2

kubectl rollout status deployment fitness-booking
```

Параллельно в отдельном терминале гонять curl-цикл, как в сценарии A —
благодаря `maxUnavailable: 0` / `maxSurge: 1` в `k8s/deployment.yaml`
в любой момент времени должен быть хотя бы один старый или новый `Ready`
под, обслуживающий трафик.

## Шаг 8. Убрать за собой

```bash
kubectl delete -f k8s/
kind delete cluster --name fitness-lab2   # или: minikube delete
docker compose down -v
```

---

## Что положить в отчёт (сводка)

1. Размеры образов из Шага 2 (`docker images`) — однослойный vs multi-stage,
   и объяснение почему это важно не только косметически.
2. Скриншот/вывод `docker compose ps` со статусом `healthy` у обоих
   сервисов (Шаг 3).
3. Вывод `kubectl get pods` с двумя `Ready` репликами (Шаг 5).
4. Логи/скриншоты минимум двух сценариев из Шага 7 с объяснением, что
   именно произошло и почему `docker-compose` эту проблему решить не может
   (нет концепции реплик/самолечения/service discovery с health-aware
   балансировкой — в compose это либо руки, либо внешний оркестратор,
   которым и является Kubernetes).
5. Обоснование выбора liveness=HTTP / readiness=gRPC (Шаг 6) и версия
   вашего кластера (`kubectl version`), которая определила, можно ли
   использовать нативную gRPC-пробу.
