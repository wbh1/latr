.PHONY: test
test:
	go test ./...

.PHONY: test-e2e
test-e2e:
	@echo "Checking docker compose..."
	@command -v docker >/dev/null 2>&1 || { echo "docker is required but not installed"; exit 1; }
	@docker compose version >/dev/null 2>&1 || { echo "docker compose is required but not installed"; exit 1; }
	@echo "Running e2e tests..."
	go test -tags=e2e ./test/e2e -v -timeout=5m

.PHONY: test-all
test-all: test test-e2e
