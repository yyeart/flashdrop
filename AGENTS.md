# Agent instructions

## Context pointers

- Domain language: read `CONTEXT.md` before naming or changing domain concepts.
- Architecture: read `docs/architecture.md` before changing module seams, transactions, concurrency, authentication, Redis, SSE, gRPC, or Kafka.
- Roadmap: read `docs/plan/roadmap.md` before starting a feature; implement only the current milestone and its acceptance criteria.
- Decisions: read relevant files under `docs/adr/` before revisiting an architectural choice.
- Issue tracker: read `docs/agents/issue-tracker.md` before creating, reading, or updating work items.
- Domain-doc rules: read `docs/agents/domain.md` before editing `CONTEXT.md` or ADRs.

## Working rules

- Implement vertical slices through deep module interfaces; keep transaction and state-machine details inside the owning module.
- Treat PostgreSQL as the source of business correctness. Redis may protect or accelerate a request but must not own inventory or durable idempotency.
- Propagate `context.Context` through HTTP, storage, Redis, workers, and later gRPC/Kafka operations.
- Give every goroutine an owner, cancellation path, bounded queue, and shutdown wait.
- Keep SSE best-effort; authoritative recovery uses read endpoints.
- Add or update tests with every behavior change, including integration and race tests for concurrency-sensitive code.

## Completion

A change is complete when its milestone criteria pass, relevant documentation still agrees with behavior, `go test ./...`, `golangci-lint run ./...` pass, and concurrency-sensitive packages pass `go test -race ./...`.

## Agent skills

### Issue tracker

Issues and specs live in GitHub Issues; use the `gh` CLI after a remote is configured. See `docs/agents/issue-tracker.md`.

### Domain docs

This is a single-context repository using root `CONTEXT.md` and `docs/adr/`. See `docs/agents/domain.md`.