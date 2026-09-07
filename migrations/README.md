# Миграции БД (PostgreSQL)

Миграции описывают схему PostgreSQL для модуля `flashsale`. PostgreSQL является
источником истины для Stock, Reservation, Order и долговечной идемпотентности.

## Локальный запуск

`.env` не хранится в Git. Создайте его из примера перед запуском Makefile или
Compose:

```bash
cp .env.example .env
make db-up
make port-forward
```

`db-up` запускает PostgreSQL в Compose, а `port-forward` публикует его на
`127.0.0.1:5432`. Проверить состояние контейнеров можно так:

```bash
docker compose ps
```

## Применение и откат через `psql`

Команды ниже выполняются из корня репозитория. Флаг `ON_ERROR_STOP=1` нужен,
чтобы `psql` завершался сразу после первой ошибки, а не продолжал выполнять
частично применённую схему.

Сначала загрузите параметры из `.env` в текущую shell-сессию:

```bash
set -a
. ./.env
set +a
```

Применить миграции:

```bash
PGPASSWORD="$POSTGRES_PASSWORD" psql \
  --host=127.0.0.1 \
  --port=5432 \
  --username="$POSTGRES_USER" \
  --dbname="$POSTGRES_DB" \
  -v ON_ERROR_STOP=1 \
  -f migrations/000001_init.up.sql
```

Откатить миграции:

```bash
PGPASSWORD="$POSTGRES_PASSWORD" psql \
  --host=127.0.0.1 \
  --port=5432 \
  --username="$POSTGRES_USER" \
  --dbname="$POSTGRES_DB" \
  -v ON_ERROR_STOP=1 \
  -f migrations/000001_init.down.sql
```

Эти команды выполняют SQL напрямую и не ведут таблицу версий мигратора.
Не смешивайте их с `make migrate-up/down` на одной базе, если нужна корректная
история версий миграций.

## Мигратор `golang-migrate`

Для обычной разработки можно использовать контейнерный мигратор:

```bash
make migrate-up
make migrate-down
```

`migrate-down` откатывает одну последнюю миграцию. Новую пару файлов можно
создать так:

```bash
make migrate-create seq=add_example_table
```

## Интеграционные тесты PostgreSQL

Тесты адаптера `flashsale/postgres` работают с настоящей базой из Compose и
запускаются только явно. Перед запуском примените миграции:

```bash
make db-up
make port-forward
make migrate-up
make test-integration
```

Цель `make test-integration` собирает `FLASHDROP_TEST_DSN` из
`POSTGRES_USER`, `POSTGRES_PASSWORD` и `POSTGRES_DB` в `.env`. Для подключения
с хоста она использует `127.0.0.1:5432` и `search_path=flashdrop,public`.

Обычный `go test ./...` не подключается к базе: интеграционные тесты
пропускаются, если `FLASHDROP_TEST_DSN` не задана. Поэтому для
`make test-integration` нужны `.env`, запущенный PostgreSQL и применённые
миграции. Тесты генерируют новые UUID для каждого сценария и удаляют только
созданные ими строки через `testing.T.Cleanup`; `make db-clean` для них не
нужен.

## Инварианты базы данных

Ниже зафиксирован контракт Milestone 2. Соответствующие базовые ограничения
должны находиться в SQL-миграции, а не только в Go-коде:

- UUID первичных ключей уникальны; внешние ключи не используют `ON DELETE CASCADE`.
- `users.role` принимает только `user` или `admin`.
- `sales.state` принимает только `draft`, `active` или `ended`; `starts_at < ends_at`.
- Состояния Reservation ограничены `pending`, `paid`, `cancelled`, `expired`; его
  количество положительно, а `created_at < expires_at`.
- Цена хранится в `BIGINT` как minor units; `total_qty > 0`, `reserved_qty >= 0`, `sold_qty >= 0` и
  удовлетворяют `reserved_qty + sold_qty <= total_qty`.
- Для Reservation существует не более одного Order.
- Идемпотентный ключ уникален в пределах `user_id`; запись хранит результат и
  отпечаток входного запроса, чтобы отличать повтор того же запроса от конфликта
  payload.

## Инварианты приложения

База не заменяет бизнес-логику приложения. В Go-модуле остаются:

- переходы Sale и Reservation по state machine;
- reserve в одной транзакции с условным обновлением Stock, созданием Reservation
  и сохранением idempotency result;
- `pay/cancel/expire` с блокировкой Reservation и проверкой `pending`;
- согласованность `user_id`, `sale_item_id` и `quantity` между Reservation и Order;
- генерация UUID и timestamps, если они не имеют DB default;
- отправка уведомлений только после успешного commit.

## Остановка и очистка

```bash
make port-close
make db-down
```

Для полного сброса локальной базы с удалением named volume:

```bash
make db-clean
```

`db-clean` удаляет все данные локального PostgreSQL и требует подтверждения.
