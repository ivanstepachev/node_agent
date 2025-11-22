# VPN Node Metrics Agent

Легковесный Go-агент, который запускается рядом с Xray/Sing-Box нодой (обычно в Docker) и раз в **3–5 секунд** отправляет на backend основную телеметрию сервера:

- `cpu_idle` — вычисляется на основе `/proc/stat`;
- `softirq_percent` — относительная загрузка softirq (дельта из `/proc/softirqs`, нормализованная на CPU ticks);
- `bw_in_mbps` / `bw_out_mbps` — входящий/исходящий трафик во всех WAN-интерфейсах (дельта из `/proc/net/dev`);
- `timestamp` — UNIX-время отправки.

## Основные возможности

- Авто-детект WAN-интерфейсов (игнорируются `lo`, `docker*`, `br-*`, `veth*`, `virbr*`, `tun*/tap*`, `wg*`, `zt*`, `ham*`, интерфейсы без глобального IP, MTU < 1000, не `UP`).
- Съём всех метрик без паник: ошибки чтения `/proc/*` и сетевые ошибки логируются с префиксами `[WARN]` / `[ERROR]`, агент продолжает работу.
- REST-отправка по `POST {BACKEND_URL}/metrics/report` с JSON-пейлоадом вида:

```json
{
  "server_id": "srv123",
  "cpu_idle": 92.5,
  "softirq_percent": 7.3,
  "bw_in_mbps": 120.4,
  "bw_out_mbps": 98.7,
  "timestamp": 1738301222
}
```

- Поддержка токена авторизации (`Authorization: Bearer ...`), тайм-аут HTTP — 4 секунды.
- Готовность к расширению (командный канал, управление конфигами VPN) через отдельные internal-пакеты.

## Быстрый старт

```bash
git clone <repo>
cd node_agent
BACKEND_URL=https://backend.example \
SERVER_ID=srv-01 \
REPORT_INTERVAL=5 \
go run ./cmd/agent
```

### Переменные окружения

| Переменная        | Описание                                     | Значение по умолчанию |
|-------------------|----------------------------------------------|-----------------------|
| `BACKEND_URL`     | Базовый URL backend-сервиса (без `/metrics`) | — (обязательно)       |
| `SERVER_ID`       | Уникальный ID сервера/ноды                   | — (обязательно)       |
| `REPORT_INTERVAL` | Интервал отчёта (секунды, 3–30)              | `5`                   |
| `AGENT_TOKEN`     | (опционально) Bearer-токен                   | пусто                 |

### Docker

```bash
docker build -t ivanstepachev/node_agent .
docker run -d --name node_agent --restart unless-stopped \
  --net host \
  -e BACKEND_URL=https://bla1.requestcatcher.com/test \
  -e SERVER_ID=460 \
  -e REPORT_INTERVAL=5 \
  -e AGENT_TOKEN=secret-token \
  ivanstepachev/node_agent
```

#### Docker Compose

Есть готовый `docker-compose.yml`:

```bash
BACKEND_URL=https://backend.example \
SERVER_ID=srv-01 \
REPORT_INTERVAL=5 \
docker compose up -d
```

> Агент читает `/proc/*`, поэтому контейнеру нужен доступ к host PID/network namespace (например, `--net host`).

## Логи

```
[INFO] agent started interval=5s backend=https://backend.example
[INFO] metrics sent: cpu_idle=93.2 softirq=4.1 bw_in=210.4 bw_out=188.7
[WARN] send metrics: backend responded with status 500 Internal Server Error
```

## Тестирование и отладка

- `go test ./...` — предусмотрены чистые internal-пакеты без глобального состояния, что упрощает покрытие тестами.
- Для проверки bandwidth-логики удобно запускать агент на локальной машине и генерировать трафик `iperf3`.

## Дальнейшее расширение (v2 Roadmap)

- Приём управляющих команд с backend (`create_config`, `delete_config`).
- Инвентаризация активных конфигов Xray/Sing-Box, сравнение с эталонным списком и diff-применение.
- Экспорт Prometheus-метрик и gRPC API для локальной автоматизации.

Агент готов к эксплуатации на VPS-провайдерах Hetzner, OVH, UltaHost, DigitalOcean, AWS, Contabo и bare-metal серверах с ядрами Linux 4.x–6.x. Потребление CPU < 1%, память < 20 МБ.