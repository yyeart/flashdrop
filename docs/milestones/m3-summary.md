# Milestone 3 — итоговая проверка

Дата итоговой повторной проверки: 2026-10-12. Статус: выполнено; блокирующие замечания ревью устранены.

Проверка выполнена на ветке `m3-http-jwt-middleware` при HEAD `80c78df`. Результаты относятся к рабочему дереву с незакоммиченными исправлениями и тестами, включая `TestContract_QuantityInputs`, а не только к содержимому HEAD. Локальная версия Go: `go1.27.0 darwin/arm64`.

## Реализованный срез

| Критерий | Реализация и проверка |
|---|---|
| OpenAPI-контракт | [OpenAPI 3.1.2](../../api/openapi/openapi.yaml) описывает 12 операций; [contract tests](../../internal/httpapi/contract_test.go) валидируют документ, запросы и ответы для каждой операции, а также выборочные ошибки. |
| Ручные HTTP handlers | [router](../../internal/httpapi/router.go) связывает auth, Sale, Reservation и Order handlers с маршрутами контракта. |
| Register/login и роли | [identity service](../../internal/identity/service.go) обслуживает регистрацию и login; [Register](../../internal/identity/register.go) всегда назначает `user`. Отдельная команда `seed-admin` запускается из [main.go](../../cmd/flashdrop/main.go). |
| Ed25519 access JWT | [token.go](../../internal/identity/token.go) выпускает EdDSA JWT с `sub`, `role`, `iss`, `aud`, `iat`, `exp`; [authenticate.go](../../internal/identity/authenticate.go) проверяет их. [Authenticate tests](../../internal/identity/authenticate_test.go) отклоняют просроченные claims, неверную подпись, алгоритм и некорректные claims. |
| Middleware и RBAC | [router.go](../../internal/httpapi/router.go) применяет recovery → request ID → logging → timeout → rate limit; защищённые маршруты затем проходят authentication → RBAC. [RBAC tests](../../internal/httpapi/foundation_test.go) проверяют доступ `user` и `admin`. |
| Единый формат ошибок | [errors.go](../../internal/httpapi/errors.go) формирует `error.code`, `error.message`, `error.request_id`; [contract tests](../../internal/httpapi/contract_test.go) сверяют ошибки с OpenAPI и заголовком `X-Request-ID`. |
| Запуск приложения | [main.go](../../cmd/flashdrop/main.go) собирает PostgreSQL stores, identity service, handlers и router. [server.go](../../internal/httpapi/server.go) реализует graceful shutdown; [server tests](../../internal/httpapi/server_test.go) проверяют завершение активного запроса и timeout. |

## Закрытые замечания ревью

- [Timeout middleware](../../internal/httpapi/middleware_common.go) устанавливает deadlines чтения и записи с учётом более раннего родительского deadline. Для записи ответа предусмотрена дополнительная секунда (`responseWriteGrace`); после начала ответа error envelope не дописывается. [Сетевые тесты](../../internal/httpapi/timeout_network_test.go) проверяют зависшее чтение тела и клиента, который не читает ответ; [unit-тесты](../../internal/httpapi/timeout_test.go) — родительский deadline, отказ установки deadline и уже начатый ответ.
- [Разбор количества](../../internal/httpapi/qty_input.go) принимает целочисленные JSON-числа, включая `1.0` и `1e0`, без преобразования через `float64`; допустимый диапазон — от 1 до 2147483647. Границы и некорректные значения покрыты [unit-тестами](../../internal/httpapi/qty_input_test.go). `TestContract_QuantityInputs` в [contract_test.go](../../internal/httpapi/contract_test.go) сверяет запросы и ответы с OpenAPI для `reserveSaleItem` и `addSaleItem`: проверяет передачу количества в сервис и отклонение дроби, переполнения, `null` и строки без вызова сервиса.

## Проверки

На локальном рабочем дереве завершились с кодом 0:

```text
go test ./... -count=1                  PASS
go test -race ./... -count=1            PASS
golangci-lint run ./...                  0 issues
make test-integration GOFLAGS=-race     PASS
sh scripts/generate-jwt-keys_test.sh    PASS
```

`FLASHDROP_TEST_DSN` не был задан при общих запусках `go test`, поэтому PostgreSQL integration tests запускались отдельно через `make test-integration GOFLAGS=-race`. Этот target из [Makefile](../../Makefile) задаёт DSN и проверяет пакеты `cmd/flashdrop`, `internal/flashsale/postgres` и `internal/identity/postgres` на локальной PostgreSQL; `GOFLAGS=-race` включает race detector. Запуск сервера и его остановка с реальной БД также проверяются в [server_test.go](../../cmd/flashdrop/server_test.go) при заданном `FLASHDROP_TEST_DSN`. [Тест скрипта JWT-ключей](../../scripts/generate-jwt-keys_test.sh) запускался отдельно.

## Устройство и границы

HTTP-запрос проходит глобальные middleware, затем для защищённых маршрутов аутентификацию и проверку роли. Handler вызывает соответствующий модуль; durable состояние и идемпотентность хранятся в PostgreSQL. Конфигурацию сервера загружает [LoadServer](../../internal/config/config.go); ключи для локальной разработки создаёт [generate-jwt-keys.sh](../../scripts/generate-jwt-keys.sh).

В текущем бинарнике используется `AllowAllRateLimiter`: счётчики Redis и политика отказа при его недоступности относятся к Milestone 4. Там же запланированы reaper, notifications и SSE. Health endpoints и Testcontainers относятся к Milestone 5. PostgreSQL integration tests пока запускаются локально; [CI](../../.github/workflows/ci.yaml) не поднимает для них БД. Ручной smoke test через последовательность curl-запросов в рамках этой итоговой документационной проверки не повторялся.

Gate из [roadmap](../plan/roadmap.md) выполнен: contract tests проходят, обычная регистрация назначает `user`, expired/invalid JWT отклоняется. Milestone 3 закрыт. Следующий этап — Milestone 4: Redis, reaper и SSE.
