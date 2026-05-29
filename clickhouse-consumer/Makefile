include .env
export

.PHONY: run test lint

run:
	go run ./cmd/consumer

test:
	go test ./... -v -count=1

lint:
	golangci-lint run -v --color always
