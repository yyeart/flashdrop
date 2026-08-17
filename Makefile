.PHONY: test lint

test:
	@go test ./... -cover

lint:
	@golangci-lint run ./...

lint-fix:
	@golangci-lint run ./... --fix