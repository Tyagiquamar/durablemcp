.PHONY: fmt test race build run-server run-executor run-scheduler compose-up compose-down compose-config

fmt:
	go fmt ./...

test:
	go test ./...

race:
	go test -race ./...

build:
	go build ./...

run-server:
	go run ./cmd/server

run-executor:
	go run ./cmd/executor

run-scheduler:
	go run ./cmd/scheduler

compose-up:
	docker compose up --build

compose-down:
	docker compose down -v

compose-config:
	docker compose config
