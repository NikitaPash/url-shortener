.PHONY: up down rebuild logs ps test coverage

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
