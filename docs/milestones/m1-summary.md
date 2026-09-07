# Milestone 1 — итоговая проверка

Дата проверки: 2026-08-18.

Этот документ фиксирует состояние именно на дату проверки. Замечания о
`float64` и отсутствии persistence rehydration были устранены до закрытия
Milestone 2; актуальный результат описан в [m2-summary.md](m2-summary.md).

Milestone 1 проверен после изменения lifecycle API. Исходный Go-код в рамках этой проверки не изменялся.

Последний индекс проекта: 258 узлов и 991 связь. Индексатор не обнаружил `parse_partial` или пропущенных исходных файлов. Каталог `milestones/` исключён из индекса намеренно через `.gitignore`, поэтому этот документ не участвует в графе.

## 1. Domain model

| Сущность | Состояния и переходы | Реализация | Покрытие | Ограничение |
|---|---|---|---|---|
| Sale | `draft → active → ended`. `ended` — terminal state. Повторная активация и завершение запрещены. | [`sale.go`](../internal/flashsale/sale.go#L11), [`Activate`](../internal/flashsale/sale.go#L194), [`End`](../internal/flashsale/sale.go#L242) | [`sale_test.go`](../internal/flashsale/sale_test.go#L443), [`sale_test.go`](../internal/flashsale/sale_test.go#L566), [`sale_test.go`](../internal/flashsale/sale_test.go#L716), [`sale_test.go`](../internal/flashsale/sale_test.go#L757) | `Activate` разрешён до `startsAt`; это закреплено тестом, но отдельно не объяснено в доменной документации. `price` пока `float64`, имя и цена позиции не валидируются. |
| Reservation | `pending → paid`, `pending → cancelled`, `pending → expired`. `paid`, `cancelled`, `expired` — terminal states. | [`reservation.go`](../internal/flashsale/reservation.go#L10), [`Pay`](../internal/flashsale/reservation.go#L108), [`Cancel`](../internal/flashsale/reservation.go#L122), [`Expire`](../internal/flashsale/reservation.go#L136) | [`reservation_test.go`](../internal/flashsale/reservation_test.go#L204), [`reservation_test.go`](../internal/flashsale/reservation_test.go#L222), [`reservation_test.go`](../internal/flashsale/reservation_test.go#L240), [`reservation_test.go`](../internal/flashsale/reservation_test.go#L389) | Граница `expiresAt` покрыта. Ошибка `Expire` до срока использует `ErrForbiddenTransition`, а не отдельную ошибку временного окна. |
| Order | Состояний нет. `Order` — неизменяемая запись завершённой покупки, а не state machine. | [`order.go`](../internal/flashsale/order.go#L10), [`CONTEXT.md`](../CONTEXT.md#L21) | [`order_test.go`](../internal/flashsale/order_test.go#L12), [`order_test.go`](../internal/flashsale/order_test.go#L51), проверки полей и невалидных входов в [`order_test.go`](../internal/flashsale/order_test.go#L96) | `newOrder` не экспортируется: [`order.go`](../internal/flashsale/order.go#L43). Для PostgreSQL adapter потребуется factory или rehydration seam. |

## 2. Конфигурация

| Параметр | Default и допустимые значения | Ошибки | Реализация и тесты | Ограничение |
|---|---|---|---|---|
| `LOG_LEVEL` | При отсутствии — `info`. Поддерживаются `debug`, `info`, `warn`, `error`; регистр игнорируется. | Пустое значение и неизвестный уровень, включая `TRACE` и `WARNING`. | Чтение и parsing: [`config.go`](../internal/config/config.go#L26). Валидные значения: [`config_test.go`](../internal/config/config_test.go#L10). Ошибки: [`config_test.go`](../internal/config/config_test.go#L72). | Пробелы не обрезаются. `APP_ENV` в Milestone 1 отсутствует намеренно. |
| `SHUTDOWN_TIMEOUT` | При отсутствии — `10s`. Значение разбирается как `time.Duration` и должно быть `> 0`. | Пустое значение, ошибка синтаксиса duration, `0s` и отрицательные значения. | [`config.go`](../internal/config/config.go#L48), [`config_test.go`](../internal/config/config_test.go#L100) | Верхняя граница не задана. `app.Run` отдельно не валидирует duration; validation выполняет config loader. |
| Ошибка загрузки | `config.Load` возвращает wrapped error. `main` пишет ошибку bootstrap logger’ом и завершает процесс с кодом `1`; workers до этого не запускаются. | Проверяется wrapper ошибки, но не полноценный subprocess с проверкой exit code. | [`Load`](../internal/config/config.go#L17), [`main`](../cmd/flashdrop/main.go#L27), [`config_test.go`](../internal/config/config_test.go#L137) | Прямого integration-теста для `main` нет. |
| Неизменяемость | Конфигурация загружается один раз значением; reload API отсутствует. | — | [`Config`](../internal/config/config.go#L12), загрузка в [`main.go`](../cmd/flashdrop/main.go#L27) | Поля структуры экспортированы, поэтому это semantic immutability, а не запрет изменения на уровне типов. |

## 3. Logger

| Пункт | Поведение | Где реализовано | Тест | Ограничение |
|---|---|---|---|---|
| Уровень | `slog.HandlerOptions.Level` получает уровень из `Config`. | [`NewLogger`](../internal/appLogger/logger.go#L8) | Фильтрация уровней: [`logger_test.go`](../internal/appLogger/logger_test.go#L10) | Нет отдельного теста полного пути `environment → config → main → logger`. |
| Handler | Используется `slog.NewTextHandler`. | [`logger.go`](../internal/appLogger/logger.go#L14) | Проверка текстового вывода: [`logger_test.go`](../internal/appLogger/logger_test.go#L61) | Формат логов пока не является внешним контрактом. |
| stderr | Bootstrap и основной logger создаются с `os.Stderr`. | [`main.go`](../cmd/flashdrop/main.go#L24), [`main.go`](../cmd/flashdrop/main.go#L33) | Unit-тесты используют `bytes.Buffer`, прямой stderr-тест отсутствует. | Проверка stderr остаётся ручной или subprocess-проверкой. |

## 4. Lifecycle и graceful shutdown

Публичный контракт:

```go
Run(ctx, shutdownTimeout, workers...) error
```

Он реализован в [`internal/app/lifecycle.go`](../internal/app/lifecycle.go#L17), а entry point передаёт timeout и workers через [`cmd/flashdrop/main.go`](../cmd/flashdrop/main.go#L36).

| Требование | Фактическое поведение | Тест |
|---|---|---|
| `0 workers → nil` | При пустом variadic списке `Run` немедленно возвращает `nil`; goroutine и timer не создаются. | [`TestRun_NoWorkersReturnsNil`](../internal/app/lifecycle_test.go#L12) |
| Каждый worker запускается ровно один раз и параллельно | Для каждого элемента создаётся ровно одна goroutine. Цикл запуска не ждёт завершения предыдущего worker. | [`TestRun_WaitsForAllWorkers`](../internal/app/lifecycle_test.go#L20) проверяет старт двух workers. |
| `Run` ждёт всю группу | Локальный `sync.WaitGroup` получает `Add(len(workers))`; каждый worker вызывает `Done`; supervisor закрывает `allDone` после `wg.Wait()`. | [`lifecycle.go`](../internal/app/lifecycle.go#L26), [`TestRun_WaitsForAllWorkers`](../internal/app/lifecycle_test.go#L20), [`TestRun_CancellationWaitsForAllWorkers`](../internal/app/lifecycle_test.go#L63) |
| Timeout общий для группы | Timer создаётся один раз после отмены root context и ограничивает ожидание всей группы, а не каждого worker. До cancellation timeout не действует. | [`lifecycle.go`](../internal/app/lifecycle.go#L43), [`TestRun_DoesNotApplyShutdownTimeoutBeforeCancellation`](../internal/app/lifecycle_test.go#L161) |
| Уже отменённый context | Workers всё равно запускаются. Они получают уже отменённый context и могут выполнить cleanup; shutdown timeout начинается после перехода в shutdown-фазу. | [`TestRun_AlreadyCancelledContextStillStartsAllWorkers`](../internal/app/lifecycle_test.go#L201) |
| Нормальное завершение | Если `allDone` закрыт до timeout, `Run` возвращает `nil`. | [`TestRun_CancellationWaitsForAllWorkers`](../internal/app/lifecycle_test.go#L63) |
| Timeout | Если группа не завершилась в общий timeout, возвращается ошибка, оборачивающая `ErrShutdownTimeout`. Перед возвратом выполняется финальная проверка `allDone`. | [`ErrShutdownTimeout`](../internal/app/lifecycle.go#L11), [`lifecycle.go`](../internal/app/lifecycle.go#L52), [`TestRun_ReturnsShutdownTimeoutWhenWorkerDoesNotFinish`](../internal/app/lifecycle_test.go#L108) |
| Worker после timeout | Goroutine не уничтожается принудительно. `Run` возвращает ошибку, а worker может продолжить работу и завершиться позже. | Поведение явно проверено в [`TestRun_ReturnsShutdownTimeoutWhenWorkerDoesNotFinish`](../internal/app/lifecycle_test.go#L108). |
| Exit code | `main` логирует ошибку `Run` и завершает процесс через `os.Exit(1)`. Ошибка config обрабатывается тем же кодом выхода. | [`main.go`](../cmd/flashdrop/main.go#L27), [`main.go`](../cmd/flashdrop/main.go#L44) |

`ErrShutdownTimeout` — sentinel для `errors.Is`; он относится к lifecycle, а не к configuration.

## 5. Команды проверки

Проверены команды:

| Команда | Результат |
|---|---|
| `go test ./...` | Пройдено для всех пакетов. |
| `go test -race ./...` | Пройдено для всех пакетов. |
| `go vet ./...` | Пройдено, ошибок нет. |
| `gofmt -l (rg --files -g '*.go')` в PowerShell | Не вывело файлов (`NO_FORMAT_FILES`). |

Дополнительно:

- `make test` пройдено; цель определена в [`Makefile`](../Makefile#L3).
- `make lint` пройдено, `golangci-lint` сообщил `0 issues`; цель определена в [`Makefile`](../Makefile#L6).

## 6. Выполненный gate Milestone 1

- [x] Git и Go module существуют, бинарник находится в `cmd/flashdrop`.
- [x] Sale, Reservation и Order имеют зафиксированные доменные инварианты и тесты.
- [x] Terminal states не допускают дальнейших переходов.
- [x] Конфигурация загружается отдельным loader’ом с defaults и validation.
- [x] Logger использует `slog`, `TextHandler` и stderr.
- [x] Root context создаётся в `main` через signal-aware context.
- [x] `Run(ctx, timeout, workers...)` поддерживает пустую группу и несколько workers.
- [x] Все workers запускаются один раз, параллельно и получают общий context.
- [x] Shutdown ждёт всю группу через `WaitGroup` и `allDone`.
- [x] Общий timeout начинается только после отмены root context.
- [x] Timeout возвращает `ErrShutdownTimeout`, а exit code обрабатывается в `main`.
- [x] Unit, race, vet и format checks проходят.

## 7. Ограничения, сознательно отложенные

- Goroutine нельзя принудительно остановить; timeout ограничивает ожидание, но не жизнь worker.
- После timeout `main` должен завершить процесс; `app.Run` сам процесс не завершает.
- Worker-level error aggregation, panic recovery и отдельная политика ошибок workers не входят в M1: `Worker` не возвращает error.
- Нет subprocess-тестов для реального signal delivery и exit code.
- PostgreSQL, HTTP, Redis, JWT, middleware и конкурентный reserve относятся к последующим milestones.
- Общий lifecycle остаётся in-process; распределённый shutdown относится ко второму этапу.

## 8. Готовность к Milestone 2

Milestone 1 можно закрывать. К Milestone 2 проект готов как к следующему учебному срезу, но до написания PostgreSQL migrations нужно принять два решения:

1. **Денежный тип.** `SaleItem.price` сейчас `float64`; перед схемой PostgreSQL нужно выбрать integer minor units либо другой явно определённый денежный тип.
UPD: решили примерно так
```go
type Money struct {
    AmountMinor int64
    Currency Currency // необязательно
}
```
Нужно зафиксировать:
- допустима ли отрицательная цена;
- какая валюта используется;
- масштаб minor units;
- где происходит парсинг "12.50" в int64;
- правила округления.
2. **Factory/rehydration для Order.** `newOrder` сейчас непубличный; нужно определить API, через которое PostgreSQL adapter будет безопасно создавать `Order` из сохранённой записи.
UPD: решили примерно так:
- отдельные factory для создания и восстановления
```
NewOrder(input) → Order
RehydrateOrder(snapshot) → Order
```
NewOrder используется Pay use case, RehydrateOrder — PostgreSQL adapter.

Без этих решений можно начать подготовку структуры Milestone 2, но не стоит фиксировать окончательную SQL-схему и adapter API: оба решения затронут persistence-контракт.
