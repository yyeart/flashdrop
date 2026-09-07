# Milestone 2 — итоговая проверка

Дата проверки: 2026-09-07.

Статус: выполнено.

Проверка выполнена на ветке `m2-postgres-and-flashsale`, commit `f66c4a6`.
Рабочее дерево было чистым. Индекс codebase-memory был обновлён перед проверкой:
511 узлов, 3175 связей, пропущенных исходных файлов нет. SQL-миграция имеет
best-effort отметку частичного разбора строк 68–70; эти строки дополнительно
проверены напрямую и содержат закрытие `idempotency_records`.

## 1. Критерии Milestone 2

| Критерий | Реализация и проверка | Результат |
|---|---|---|
| Migrations для users, sales, sale_items, reservations и orders | [`000001_init.up.sql`](../../migrations/000001_init.up.sql), [`000001_init.down.sql`](../../migrations/000001_init.down.sql) | Выполнено |
| PostgreSQL adapter для flashsale | [`internal/flashsale/postgres/store.go`](../../internal/flashsale/postgres/store.go), [`sale_item.go`](../../internal/flashsale/postgres/sale_item.go), [`sale_lifecycle.go`](../../internal/flashsale/postgres/sale_lifecycle.go), [`reserve.go`](../../internal/flashsale/postgres/reserve.go), [`pay.go`](../../internal/flashsale/postgres/pay.go), [`lifecycle.go`](../../internal/flashsale/postgres/lifecycle.go), [`order.go`](../../internal/flashsale/postgres/order.go) | Выполнено |
| Reserve: условный UPDATE Stock, Reservation и idempotency result в одной транзакции | [`Reserve`](../../internal/flashsale/postgres/reserve.go#L49), [`reserveStock`](../../internal/flashsale/postgres/reserve.go#L201), [`persistReservationAndResult`](../../internal/flashsale/postgres/reserve.go#L237) | Выполнено |
| Pay/cancel/expire: row lock и проверка pending state | [`Pay`](../../internal/flashsale/postgres/pay.go#L14), [`Cancel`](../../internal/flashsale/postgres/lifecycle.go#L14), [`Expire`](../../internal/flashsale/postgres/lifecycle.go#L54), [`selectReservation`](../../internal/flashsale/postgres/lifecycle.go#L94) | Выполнено |
| Уникальный idempotency key и один Order на Reservation | Ограничения [`000001_init.up.sql`](../../migrations/000001_init.up.sql#L53) и [`000001_init.up.sql`](../../migrations/000001_init.up.sql#L62), проверки replay/conflict в [`reserve_integration_test.go`](../../internal/flashsale/postgres/reserve_integration_test.go#L233) и one-order поведения в [`pay_integration_test.go`](../../internal/flashsale/postgres/pay_integration_test.go#L103) | Выполнено |
| Gate: Stock 100, 1000 конкурентных reserve, ровно 100 успехов | [`TestStore_Reserve_ConcurrentRequestsDoNotOversell`](../../internal/flashsale/postgres/reserve_integration_test.go#L613) | Выполнено: 100 успехов, 900 отказов, `reserved_qty=100`, 100 Reservation |

## 2. Проверки

Пройдены:

```text
go test ./...                                      PASS
go test -race ./...                                PASS
go vet ./...                                       PASS
golangci-lint run ./...                            0 issues
make test-integration                              PASS
go test -race ./internal/flashsale/postgres -count=1 PASS
```

Обычный `go test ./...` не включает PostgreSQL integration tests без
`FLASHDROP_TEST_DSN`; поэтому integration gate запускался отдельно через
`make test-integration` на PostgreSQL из Compose. Это соответствует инструкции
в [`migrations/README.md`](../../migrations/README.md#L82).

На момент проверки локальная база имела migration version `1` с `dirty=false`,
а schema `flashdrop` содержала таблицы `idempotency_records`, `orders`,
`reservations`, `sale_items`, `sales` и `users`.

## 3. Ограничения после закрытия

- Основной GitHub Actions test job намеренно исключает PostgreSQL adapter и
  прямо указывает, что integration coverage будет добавлена с Testcontainers:
  [`.github/workflows/ci.yaml`](../../.github/workflows/ci.yaml#L29). Поэтому
  M2 gate подтверждён локальным integration и race запуском, но пока не является
  обязательной частью CI.
- `git diff --check` сообщает trailing whitespace в нескольких изменённых SQL,
  Go и YAML строках. Это не нарушает текущие тесты или `golangci-lint`, но требует
  отдельной косметической уборки.
- HTTP, JWT, Redis, reaper, SSE и Testcontainers относятся к следующим
  milestones и не являются условиями закрытия M2.

Следующий этап — Milestone 3: OpenAPI contract, HTTP handlers, JWT и middleware.
