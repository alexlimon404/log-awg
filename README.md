# log-awg

Демон, который раз в минуту опрашивает `awg show <iface> dump` и пишет активность
пиров AmneziaWG в Postgres (handshake, endpoint, трафик).

Подробный план и обоснование решений — в [`docs/PLAN.md`](docs/PLAN.md).

## Как это работает

- Go-бинарник — демон, не cron-скрипт: сам крутит цикл, самокорректирующийся
  на границы минуты (без дрейфа `time.Ticker`), без наложения тиков.
- Раз в тик: `awg show <iface> dump` → парсинг → одна транзакция в Postgres
  (upsert пира, обновление счётчиков, запись снапшота).
- Снапшот в `peer_snapshots` пишется только если у пира `track_stats = true`
  **и** он хотя бы раз выходил на связь (`latest-handshake != 0`). Пиры,
  которые никогда не подключались, и пиры с `track_stats = false` — видны в
  таблице `peers`, но в историю активности не попадают.
- Стек: `pgx/v5` (драйвер), `sqlc` (типобезопасные запросы из `db/queries.sql`,
  сгенерированный код лежит в `internal/sqlcgen` и коммитится в репозиторий),
  `golang-migrate` (миграции).

## Требования

- Go 1.26+
- Postgres
- `awg` (amneziawg-tools) в `PATH`, чтение состояния интерфейса требует root
- [`golang-migrate`](https://github.com/golang-migrate/migrate) CLI — для применения миграций
- [`sqlc`](https://sqlc.dev) — только если меняете `db/queries.sql`/`db/migrations`
  и нужно перегенерировать `internal/sqlcgen`

## Быстрый старт

```bash
# 1. Применить миграции
migrate -path db/migrations -database "postgres://user:pass@localhost:5432/log_awg?sslmode=disable" up

# 2. Собрать
go build -o log-awg ./cmd/log-awg

# 3. Настроить окружение
cp deploy/env.example .env
# отредактировать .env: DATABASE_URL, при необходимости AWG_INTERFACE/AWG_BIN

# 4. Запустить (нужен root для чтения состояния интерфейса)
set -a; source .env; set +a
sudo -E ./log-awg
```

## Конфигурация

Всё через переменные окружения:

| Переменная          | Обязательна | По умолчанию | Описание                                   |
|---------------------|-------------|---------------|---------------------------------------------|
| `DATABASE_URL`       | да          | —             | DSN Postgres, `postgres://user:pass@host:port/db?sslmode=disable` |
| `AWG_INTERFACE`      | нет         | `awg0`        | Имя интерфейса AmneziaWG                    |
| `AWG_BIN`            | нет         | `awg`         | Путь/имя бинарника `awg` в `PATH`           |
| `AWG_EXEC_TIMEOUT`   | нет         | `5s`          | Таймаут на выполнение `awg show dump`       |
| `AWG_POLL_INTERVAL`  | нет         | `1m`          | Интервал опроса                             |

## Управление трекингом пира

Отключить запись статистики для конкретного пира (запись в `peers` остаётся,
`peer_snapshots` — больше не пишется):

```sql
UPDATE peers SET track_stats = false WHERE public_key = '<pubkey>';
```

## Деплой на сервер

Модель простая, pull-based: репозиторий один раз клонируется прямо на сервере в
`/var/www/log-awg`, а каждый следующий деплой — это `git pull` + пересборка +
рестарт, запущенные на самом сервере. Ничего никуда по SSH не заливается.

### Разовая настройка сервера (один раз)

```bash
ssh user@your-server

sudo git clone git@github.com:alexlimon404/log-awg.git /var/www/log-awg
sudo chown -R www-data:www-data /var/www/log-awg
cd /var/www/log-awg

# конфиг — отдельно от репозитория, там реальный пароль от БД
sudo cp deploy/env.example env
sudo nano env                            # вписать реальный DATABASE_URL
sudo chown root:root env && sudo chmod 600 env   # www-data этот файл не должен читать

migrate -path db/migrations -database "$(grep DATABASE_URL env | cut -d= -f2-)" up

sudo cp deploy/log-awg.conf /etc/supervisor/conf.d/log-awg.conf
sudo supervisorctl reread
sudo supervisorctl update
sudo supervisorctl start log-awg
```

(нужен установленный `go` и [`migrate`](https://github.com/golang-migrate/migrate)
на сервере — `go install -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@latest`,
если ещё не стоит)

### Каждый следующий деплой

```bash
ssh user@your-server /var/www/log-awg/deploy.sh
```

[`deploy.sh`](deploy.sh) делает `git pull` от имени `www-data`, собирает бинарник
на месте (`go build -buildvcs=false -o log-awg ./cmd/log-awg`), накатывает
непримененные миграции и рестартует `log-awg` через supervisor:

```sh
#!/bin/sh
set -e
cd "$(dirname "$0")"

sudo -u www-data git pull

echo "build"
go version
sudo -u www-data go build -buildvcs=false -o log-awg ./cmd/log-awg

echo "migrate"
set -a; . ./env; set +a
migrate -path db/migrations -database "$DATABASE_URL" up

echo "restart"
sudo supervisorctl restart log-awg

echo "success"
```

Бинарник собирается от `www-data` (владелец репозитория), а сам процесс под
supervisor поднимается от `root` (`user=root` в `deploy/log-awg.conf`) — это
нужно, потому что чтение состояния интерфейса через `awg show` требует root,
а сборка кода — нет. Секрет (`DATABASE_URL`) в `log-awg.conf` не попадает: команда
там — обёртка на `sh -c`, которая перед запуском бинарника сначала подтягивает
`/var/www/log-awg/env` (root-only, chmod 600).

Логи: `tail -f /var/log/log-awg.out.log /var/log/log-awg.err.log`, статус —
`supervisorctl status log-awg`.

## Разработка

```bash
go build ./...
go vet ./...
go test ./...

# интеграционный тест internal/store гоняется только если задан DATABASE_URL
DATABASE_URL="postgres://postgres:test@localhost:15432/log_awg?sslmode=disable" go test ./internal/store/... -v

# перегенерировать sqlc-код после правки db/queries.sql или db/migrations
sqlc generate
```
