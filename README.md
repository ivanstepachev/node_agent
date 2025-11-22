# VPN Node Metrics Agent

Лёгкий Go-агент, который собирает и отправляет ключевые метрики VPN-ноды (CPU idle, softirq нагрузку и входящий/исходящий трафик по WAN-интерфейсам) на backend каждые 3–5 секунд.

## Сборка и запуск

```bash
BACKEND_URL=https://api.example.com \
SERVER_ID=srv-123 \
REPORT_INTERVAL=5 \
AGENT_TOKEN=optional-token \
go run ./cmd/node-agent
```

Рекомендуется запускать агент внутри Docker-контейнера с политикой рестартов `unless-stopped`.

## Переменные окружения

| Переменная       | Обяз.? | Описание                                                   |
|------------------|--------|------------------------------------------------------------|
| `BACKEND_URL`    | да     | Базовый URL backend (например, `https://api.example.com`). |
| `SERVER_ID`      | да     | Уникальный идентификатор VPN-сервера.                      |
| `REPORT_INTERVAL`| нет    | Интервал отправки метрик (сек, по умолчанию 5, min=3).     |
| `AGENT_TOKEN`    | нет    | Bearer-токен для авторизации на backend.                   |

## Возможности

- Автообнаружение WAN-интерфейсов (исключая виртуальные/внутренние).
- Расчёт CPU idle% и softirq% на основе `/proc/stat` и `/proc/softirqs`.
- Подсчёт пропускной способности в Mbps по `/proc/net/dev`.
- JSON POST `POST {BACKEND_URL}/metrics/report` с логированием в stdout.