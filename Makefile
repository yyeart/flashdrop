-include .env
export

.PHONY: test test-integration lint lint-fix port-forward port-close db-up db-down db-clean migrate-create migrate-action migrate-up migrate-down

test:
	@go test ./... -cover

test-integration: export FLASHDROP_TEST_DSN = postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@127.0.0.1:5432/$(POSTGRES_DB)?sslmode=disable&search_path=flashdrop,public
test-integration:
	@go test ./internal/flashsale/postgres ./internal/identity/postgres -count=1

lint:
	@golangci-lint run ./...

lint-fix:
	@golangci-lint run ./... --fix

port-forward:
	@docker compose up -d port-forwarder

port-close:
	@docker compose down port-forwarder

db-up:
	@docker compose up -d flashdrop-postgres

db-down:
	@docker compose down flashdrop-postgres

db-clean:
	@read -p "Are you sure? (y/n): " ans; \
	if [ "$$ans" = "y" ]; then \
		docker compose down -v flashdrop-postgres && \
		echo "Database cleaned"; \
	else \
		echo "Database clean canceled"; \
	fi

migrate-create:
	@if [ -z "$(seq)" ]; then \
		echo "Please provide a migration name. Usage: make migrate-create seq=<migration_name>"; \
		exit 1; \
	fi; \
	docker compose run --rm flashdrop-postgres-migrate \
		create \
		-ext sql \
		-dir /migrations \
		-seq "$(seq)"

migrate-action:
	@if [ -z "$(action)" ]; then \
		echo "Please provide an action. Usage: make migrate-action action=<up|down>"; \
		exit 1; \
	fi; \
	if [ -z "$(POSTGRES_USER)" ] || [ -z "$(POSTGRES_PASSWORD)" ] || [ -z "$(POSTGRES_DB)" ]; then \
		echo "POSTGRES_USER, POSTGRES_PASSWORD and POSTGRES_DB must be set in .env or the environment"; \
		exit 1; \
	fi; \
	docker compose run --rm flashdrop-postgres-migrate \
		-path /migrations \
		-database 'postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@flashdrop-postgres:5432/$(POSTGRES_DB)?sslmode=disable&search_path=public' \
		"$(action)" $(steps)

migrate-up:
	@make migrate-action action=up

migrate-down:
	@make migrate-action action=down steps=1
