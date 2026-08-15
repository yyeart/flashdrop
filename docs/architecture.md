# Архитектура FlashDrop

## Архитектурная форма

MVP — один бинарник и один process boundary. Внутри него находятся глубокие feature-модули с маленькими интерфейсами:

- identity — регистрация, login, access JWT и роли;
- flashsale — Sale, Sale Item, Stock, Reservation и Order;
- notifications — авторизованные SSE-подписки и доставка best-effort событий.

PostgreSQL является источником истины бизнес-состояния. Redis ускоряет и защищает запросы, но не определяет наличие Stock или уникальность долговечной команды.

~~~mermaid
flowchart LR
    Client["Клиент / Swagger UI"] --> HTTP["net/http API"]
    HTTP --> Identity["identity"]
    HTTP --> FlashSale["flashsale"]
    HTTP --> Notify["notifications / SSE"]
    Identity --> PG["PostgreSQL"]
    FlashSale --> PG
    Notify --> PG
    HTTP --> Redis["Redis"]
    FlashSale --> Redis
    FlashSale --> Reaper["DB reaper"]
    Reaper --> PG
~~~

На втором этапе process boundary появляется только после прохождения MVP gates:

~~~mermaid
flowchart LR
    Client["Клиент"] --> API["API service"]
    API -->|"gRPC + deadline + JWT metadata"| Core["Core service"]
    API --> Redis["Redis"]
    Core --> PG["PostgreSQL"]
    Core --> Outbox["Transactional outbox"]
    Outbox --> Kafka["Kafka"]
    Kafka --> API
    API --> SSE["SSE hub"]
~~~

## Module seams

### identity

Внешний интерфейс модуля предоставляет операции Register, Login и Authenticate. Реализация скрывает password hashing, Ed25519 signing/verification и role checks. Обычная регистрация всегда создаёт user; отдельная идемпотентная dev seed-команда создаёт admin.

Access JWT короткоживущий, refresh rotation в MVP отсутствует. Claims: sub, role, iss, aud, iat, exp.

### flashsale

Sale, Sale Item, Reservation и Order принадлежат одному глубокому модулю, потому что reserve/pay/cancel/expire пересекают одну транзакционную модель Stock. Наружу выходят команды и query:

- управление Sale: create, add item, activate, end;
- чтение Sale и доступного Stock;
- reserve, cancel, pay;
- чтение Order.

Соседние модули не получают доступ к репозиториям или внутренним state transitions.

### notifications

Модуль принимает события только после успешного database commit и публикует их авторизованным подписчикам. Карта подписчиков защищается RWMutex, доставка выполняется через bounded channels. Заполненный буфер отключает slow consumer и не блокирует остальных подписчиков.

SSE не является журналом событий. После reconnect клиент читает актуальное состояние через GET.

## Состояния и инварианты

- Sale: draft → active → ended; reopen запрещён.
- Reservation: pending → paid | cancelled | expired; terminal state меняется только один раз.
- Один Reservation относится к одной Sale Item и имеет положительный quantity.
- Успешный pay атомарно переводит Reservation в paid, переносит количество из reserved в sold и создаёт ровно один Order.
- Cancel и expire возвращают количество из reserved в доступный Stock.
- reserved_qty + sold_qty ≤ total_qty.
- Идемпотентная команда уникальна по user_id и idempotency_key и возвращает сохранённый результат.

## Конкурентный reserve

Reserve выполняется одной PostgreSQL-транзакцией:

1. Проверить Sale, время действия и права пользователя.
2. Условным UPDATE увеличить reserved_qty, только если доступного Stock достаточно.
3. Создать Reservation и сохранить idempotency result.
4. Выполнить commit.
5. После commit отправить best-effort notification.

Pay/cancel/expire блокируют Reservation строкой и проверяют текущее состояние перед переходом. Row-level locks PostgreSQL блокируют конфликтующие UPDATE, DELETE и SELECT FOR UPDATE до конца транзакции. [PostgreSQL explicit locking](https://www.postgresql.org/docs/current/explicit-locking.html)

## Workers и context

DB reaper — одна управляемая goroutine с периодическим batch scan просроченных Reservation. Все workers получают общий shutdown context. sync.WaitGroup.Go регистрирует задачи, а shutdown ждёт Wait. Функция, переданная в WaitGroup.Go, не должна panic. [Go sync.WaitGroup](https://pkg.go.dev/sync)

context.Context передаётся от HTTP handler к PostgreSQL, Redis и workers. Отмена запроса прекращает незавершённую работу; shutdown отменяет общий context и затем дожидается workers.

## Redis и failure policy

Redis хранит rate-limit counters и краткоживущий cache результата идемпотентной команды. Durable uniqueness и Stock остаются в PostgreSQL. Atomic rate limit допускает Lua/read-check/write; Redis документирует этот паттерн для конкурентных счётчиков. [Redis rate limiter](https://redis.io/docs/latest/develop/use-cases/rate-limiter/)

При недоступном Redis:

- public GET и reconnect SSE остаются доступными;
- login и rate-limited mutations возвращают 503;
- PostgreSQL не обходится и не заменяется fallback-хранилищем.

## HTTP и middleware

MVP использует ручные handlers на стандартном net/http. Порядок middleware:

recovery → request ID → structured logging → timeout → rate limit → authentication → RBAC.

Операции MVP: register, login, list/get Sale, reserve, cancel, pay, get Order, SSE, admin create/add/activate/end, /livez, /readyz.

OpenAPI 3.1.2 создаётся отдельным contract-first milestone перед реализацией HTTP и становится источником внешнего контракта. [OpenAPI 3.1.2](https://spec.openapis.org/oas/v3.1.2.html)

## Второй этап

После MVP Core выделяется в отдельный процесс, но продолжает владеть flashsale-логикой. API вызывает Core по gRPC с propagated deadlines; gRPC без заданного deadline может ждать неопределённо долго. [gRPC deadlines](https://grpc.io/docs/guides/deadlines/)

Бизнес-изменение и outbox event записываются одной транзакцией. Publisher доставляет события в Kafka с at-least-once semantics; consumer дедуплицирует event_id. Ключ aggregate ID сохраняет порядок только внутри Kafka partition. [Kafka documentation](https://kafka.apache.org/documentation/)

Refresh добавляется как opaque token rotation с обнаружением повторного использования по RFC 9700. [RFC 9700](https://www.rfc-editor.org/rfc/rfc9700.html)
