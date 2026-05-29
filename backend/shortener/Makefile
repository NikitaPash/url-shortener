-include .env
export

.PHONY: run test migrate-up migrate-down lint

run:
	go run ./cmd/shortener

test:
	go test ./... -v -count=1

migrate-up:
	migrate -path ./migrations -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path ./migrations -database "$(DATABASE_URL)" down 1

lint:
	golangci-lint run -v --color always
