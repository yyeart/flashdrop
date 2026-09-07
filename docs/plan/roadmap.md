# Roadmap FlashDrop

Roadmap разделяет учебный MVP и распределённый этап. Работу следует вести вертикальными срезами: каждый milestone заканчивается проверяемым gate.

## Milestone 0 — документация

Состояние: выполнено.

- Зафиксировать AGENTS.md, CONTEXT.md, README.md, architecture.md, этот roadmap и ADR.
- Зафиксировать GitHub Issues как issue tracker.
- Не создавать code skeleton, go.mod, Compose или OpenAPI на этом milestone.

Gate: все указатели из AGENTS.md существуют, доменный глоссарий не содержит implementation details, MVP и этап 2 не смешаны.

## Milestone 1 — каркас и domain model

- Инициализировать Git и Go module.
- Создать один бинарник cmd/flashdrop.
- Реализовать чистые состояния Sale, Reservation и Order.
- Добавить конфигурацию, slog, root context и graceful shutdown.

Gate: unit-тесты state machines; ни один terminal state не переходит дальше; shutdown дожидается всех зарегистрированных workers.

## Milestone 2 — PostgreSQL и flashsale

Состояние: выполнено (2026-09-07). Итоговая проверка: [m2-summary.md](../milestones/m2-summary.md).

- Создать SQL migrations для users, sales, sale_items, reservations и orders.
- Реализовать PostgreSQL adapter для flashsale.
- Reserve выполнять условным UPDATE доступного Stock и созданием Reservation в одной транзакции.
- Pay/cancel/expire сериализовать row lock и проверкой pending state.
- Зафиксировать unique idempotency key и один Order на Reservation.

Gate: при Stock 100 и 1000 конкурентных reserve по одной единице успешны ровно 100; counters и terminal transitions корректны.

## Milestone 3 — HTTP, JWT и middleware

- Перед handlers создать api/openapi/openapi.yaml версии 3.1.2.
- Реализовать ручные net/http handlers согласно контракту.
- Добавить register/login, Ed25519 access JWT, user/admin RBAC и dev seed-команду.
- Применить middleware order: recovery, request ID, logging, timeout, rate limit, authentication, RBAC.
- Стабилизировать error envelope с code, message и request_id.

Gate: contract tests сверяют handlers со спецификацией; обычная регистрация не выдаёт admin; expired/invalid token отклоняется.

## Milestone 4 — Redis, reaper и SSE

- Подключить Redis для rate-limit counters и краткоживущего idempotency cache.
- Сохранить durable uniqueness и результат идемпотентной команды в PostgreSQL.
- Реализовать один DB reaper с batch scan просроченных Reservation.
- Реализовать notifications module с bounded channels и RWMutex.
- Отключать slow consumers; не блокировать остальных подписчиков.
- Публиковать события только после commit.

Gate: Redis outage даёт 503 для login/rate-limited mutations, но reads живы; slow SSE client не блокирует hub; reconnect восстанавливается чтением GET.

## Milestone 5 — эксплуатация и интеграционные проверки MVP

- Поднять PostgreSQL и Redis для integration tests через Testcontainers.
- Добавить Docker Compose для ручного запуска стенда.
- Добавить /livez и /readyz, JSON logs и documented run commands.
- Прогнать go test ./... и go test -race ./....
- Проверить отмену HTTP context и graceful shutdown под активной нагрузкой.

Gate: весь MVP воспроизводится с нуля, обязательные concurrency и failure tests зелёные, утечек goroutine не обнаружено тестами.

## Milestone 6 — выделение Core и gRPC

- Вынести flashsale module в Core service, сохранив его интерфейс и тесты.
- Оставить API service владельцем HTTP, JWT verification и SSE.
- Описать Identity/Sales/Order RPC в protobuf.
- Добавить unary interceptors, metadata и deadlines.

Gate: API и Core запускаются отдельно; отмена/deadline исходного запроса прекращает незавершённый RPC; contract tests проходят через gRPC adapter.

## Milestone 7 — transactional outbox и Kafka

- Записывать бизнес-изменение и outbox event одной PostgreSQL-транзакцией.
- Запустить publisher с retry и backoff.
- Публиковать events по aggregate key.
- Дедуплицировать event_id на consumer side.
- Перевести API notifications с in-process source на Kafka consumer.

Gate: недоступная Kafka не ломает commit; повтор после publish не создаёт второй SSE event; порядок одинакового aggregate сохраняется в одной partition.

## Milestone 8 — refresh rotation

- Добавить opaque refresh sessions в Redis с хранением только hash.
- Выпускать новый refresh token на каждом refresh.
- Повторно использованный старый token отзывает session family.
- Добавить logout и тесты reuse detection.

Gate: refresh rotation и reuse detection покрыты integration tests; access JWT остаётся короткоживущим и проверяется Core по публичному ключу.

## Граница MVP

В MVP не входят frontend, внешний payment provider, Kubernetes, service mesh, email, tracing, Kafka, gRPC, refresh rotation и replay SSE. Любое расширение сначала оформляется issue и отдельным milestone.
