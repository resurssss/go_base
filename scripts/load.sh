#!/usr/bin/env bash
# Нагрузочная проверка: N параллельных запросов на персональную тренировку
# (интервалы не пересекаются, поэтому все должны вернуть 201). Работает на macOS и Linux.
# Использование: bash scripts/load.sh 100   (N <= 744)
N=${1:-100}
book() {
  i=$1
  day=$(printf '%02d' $((i / 24 + 1)))
  hour=$(printf '%02d' $((i % 24)))
  from="2026-01-${day}T${hour}:00:00Z"
  to="2026-01-${day}T${hour}:30:00Z"
  curl -s -o /dev/null -w "%{http_code}\n" -X POST http://localhost:8080/bookings \
    -H 'Content-Type: application/json' \
    -d "{\"slot_id\":\"personal-1\",\"client\":\"client-${i}\",\"from\":\"${from}\",\"to\":\"${to}\"}"
}
export -f book
seq 0 $((N-1)) | xargs -P 20 -I{} bash -c 'book {}' | sort | uniq -c