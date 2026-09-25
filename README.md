# FlashDrop

FlashDrop — учебный backend на Go для конкурентного резервирования ограниченного Stock.

Проект специально разбит на два этапа:

1. MVP — один модульный монолит для изучения goroutines, channels, `WaitGroup`, `RWMutex`, `context.Context`, PostgreSQL, Redis, JWT и middleware.
2. Распределённый этап — выделение API/Core, gRPC, transactional outbox, Kafka и refresh rotation.

## Ключевые решения

- PostgreSQL — источник истины для Sale, Stock, Reservation и Order.
- Redis используется для rate limit и кэша идемпотентности, но не хранит бизнес-корректность.
- MVP — один бинарник с глубокими модулями `identity`, `flashsale` и `notifications`.
- SSE — best-effort канал; актуальное состояние восстанавливается через GET.
- MVP не содержит refresh/logout и Kafka; они относятся ко второму этапу.

## Локальный запуск

Нужны Go, Docker с Compose и OpenSSL. Команды ниже выполняются из корня репозитория.

```sh
cp .env.example .env
make db-up
make port-forward
make migrate-up
make jwt-keys
make run
```

`.env` содержит настройки приложения и PostgreSQL; проверьте `DATABASE_URL` перед запуском. `make jwt-keys` создаёт пару Ed25519 в `.dev/keys/` и откажется перезаписывать существующую. Приватный ключ и `.env` не добавляйте в Git.

После запуска проверьте публичный endpoint в другом терминале:

```sh
curl -i http://127.0.0.1:8080/v1/sales
```

Чтобы создать администратора, задайте `SEED_ADMIN_EMAIL` и `SEED_ADMIN_PASSWORD` в `.env`. Команда `go run` сама не загружает этот файл, поэтому перед вызовом экспортируйте его переменные:

```sh
set -a
. ./.env
set +a
go run ./cmd/flashdrop seed-admin
```

Для проверки изменений используйте `make test`, `make test-integration` (при запущенной PostgreSQL) и `make lint`. Остановить контейнеры можно командой `docker compose down`.

## Навигация

- [Доменный язык](CONTEXT.md)
- [Архитектура](docs/architecture.md)
- [Roadmap и gates](docs/plan/roadmap.md)
- [Архитектурные решения](docs/adr/)
- [Правила issue tracker](docs/agents/issue-tracker.md)
