.PHONY: up down rebuild logs ps test coverage \
        test-infra-up test-infra-down test-integration test-integration-agent

up:
	docker compose up -d

down:
	docker compose down

rebuild:
	docker compose up -d --build

logs:
	docker compose logs -f --tail=50

ps:
	docker compose ps

test:
	cd backend/shortener && go test ./...
	cd clickhouse-consumer && go test ./...
	cd python-agent && uv run pytest tests/ -v

# Agent unit-test coverage: terminal report + browsable HTML in python-agent/htmlcov/
coverage:
	cd python-agent && uv run pytest tests/ --cov-report=html

# --- Integration tests (LOCAL ONLY — never run in CI) ----------------------------
# Infra-only harness (postgres, redis, kafka, clickhouse) for the L1/L2 suites.
test-infra-up:
	docker compose -f docker-compose.test.yml up -d --wait

test-infra-down:
	docker compose -f docker-compose.test.yml down -v

# Run ALL integration suites against an already-running harness (test-infra-up):
# both Go modules (build-tagged `integration`) and the Python agent (marked
# `integration`). All are excluded from the default `make test`.
test-integration:
	cd backend/shortener && go test -tags=integration -count=1 -v ./test/integration/...
	cd clickhouse-consumer && go test -tags=integration -count=1 -v ./test/integration/...
	cd python-agent && uv run pytest tests/integration -m integration -v

# Just the Python agent security-isolation suite (§6.7 ClickHouse row filter /
# read-only, §6.8 Redis JWT denylist). `-m integration` overrides the default
# `-m "not integration"` in pyproject so these opt-in tests are collected.
test-integration-agent:
	cd python-agent && uv run pytest tests/integration -m integration -v
