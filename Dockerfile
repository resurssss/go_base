# syntax=docker/dockerfile:1

# builder
FROM golang:1.22 AS builder

WORKDIR /src

# кэширование (чтобы не копировать полностью проект, зависимости редко обновляются)
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 — статическая линковка, без зависимости от libc рантайма (все что нужно в /out/server)
# Это позволяет использовать минимальный базовый образ на следующей стадии: бинарник ни от чего в системе не зависит.
# -o /out/server — куда положить результат: файл /out/server внутри контейнера-сборщика.
# Это и есть бинарник, в который скомпилирован весь код
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# runtime
FROM alpine:3.20

# Непривилегированный пользователь — не запускаем сервис от root
RUN adduser -D -u 10001 appuser

WORKDIR /app
COPY --from=builder /out/server /app/server

# Порты — через ENV
# значение можно переопределить снаружи, не трогая образ
ENV HTTP_PORT=8080
ENV GRPC_PORT=9090

EXPOSE 8080 9090

USER appuser

ENTRYPOINT ["/app/server"]
