package main

import (
	"log"
	"os"
	"strconv"
	"time"
)

// Config — все настройки приходят из переменных окружения.
// Значения приходят из переменных окружения (их задаёт docker-compose),
// поэтому менять число воркеров или попыток можно без перекомпиляции
type Config struct {
	RabbitURL       string
	Workers         int           // размер worker pool (сколько воркеров)
	BufferSize      int           // размер буфера chan Event (буфера канала между читателем и воркерами)
	MaxAttempts     int           // число попыток обработки до dead-letter
	BackoffBase     time.Duration // базовый интервал backoff (удваивается)
	ShutdownTimeout time.Duration // сколько ждать воркеров при остановке
	ProcessDelay    time.Duration // искусственная задержка (для демонстрации graceful shutdown)
	FailClient      string        // клиент, обработка которого всегда падает (демо dead-letter)
}

func LoadConfig() Config {
	return Config{
		RabbitURL:       getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		Workers:         getEnvInt("WORKERS", 4),
		BufferSize:      getEnvInt("BUFFER_SIZE", 16),
		MaxAttempts:     getEnvInt("MAX_ATTEMPTS", 3),
		BackoffBase:     time.Duration(getEnvInt("BACKOFF_BASE_MS", 200)) * time.Millisecond,
		ShutdownTimeout: time.Duration(getEnvInt("SHUTDOWN_TIMEOUT_SECONDS", 10)) * time.Second,
		ProcessDelay:    time.Duration(getEnvInt("PROCESS_DELAY_MS", 0)) * time.Millisecond,
		FailClient:      getEnv("FAIL_CLIENT", ""),
	}
}

// С переменными окружения достаточно изменить docker-compose.yml:
// getEnv возвращает строковую переменную окружения или значение по умолчанию
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getEnvInt возвращает числовую переменную окружения.
// Если она не задана или в ней не число / отрицательное число,
// пишет предупреждение в лог и берёт значение по умолчанию.
func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		log.Printf("invalid %s=%q, using default %d", key, v, fallback)
		return fallback
	}
	return n
}
