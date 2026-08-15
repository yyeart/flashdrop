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

## Навигация

- [Доменный язык](CONTEXT.md)
- [Архитектура](docs/architecture.md)
- [Roadmap и gates](docs/plan/roadmap.md)
- [Архитектурные решения](docs/adr/)
- [Правила issue tracker](docs/agents/issue-tracker.md)
