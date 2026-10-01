# Milestone 3 — итоговая проверка

Дата проверки: 2026-10-02. Статус: выполнено.

Проверка выполнена на ветке `m3-http-jwt-middleware` при HEAD `c0e01f9`. В рабочем дереве до документационных изменений уже была правка `internal/identity/postgres/create_user_integration_test.go`; она сохранена без изменений. Результаты команд ниже относятся к этому рабочему дереву.

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

## Проверки

На локальном рабочем дереве завершились с кодом 0:

```text
go test ./...              PASS
go test -race ./...        PASS
golangci-lint run ./...    0 issues
make test-integration      PASS
```

`FLASHDROP_TEST_DSN` не был задан при `go test ./...` и `go test -race ./...`, поэтому PostgreSQL integration tests запускались отдельно через `make test-integration`. Последняя команда проверила пакеты `cmd/flashdrop`, `internal/flashsale/postgres` и `internal/identity/postgres` на локальной PostgreSQL. Запуск сервера и его остановка с реальной БД также проверяются в [server_test.go](../../cmd/flashdrop/server_test.go) при заданном `FLASHDROP_TEST_DSN`.

## Устройство и границы

HTTP-запрос проходит глобальные middleware, затем для защищённых маршрутов аутентификацию и проверку роли. Handler вызывает соответствующий модуль; durable состояние и идемпотентность хранятся в PostgreSQL. Конфигурацию сервера загружает [LoadServer](../../internal/config/config.go); ключи для локальной разработки создаёт [generate-jwt-keys.sh](../../scripts/generate-jwt-keys.sh).

В текущем бинарнике используется `AllowAllRateLimiter`: счётчики Redis и политика отказа при его недоступности относятся к Milestone 4. Там же запланированы reaper, notifications и SSE. Health endpoints и Testcontainers относятся к Milestone 5. PostgreSQL integration tests пока запускаются локально; [CI](../../.github/workflows/ci.yaml) не поднимает для них БД. Ручной smoke test через последовательность curl-запросов в рамках этой итоговой документационной проверки не повторялся.

Следующий этап — Milestone 4: Redis, reaper и SSE.
