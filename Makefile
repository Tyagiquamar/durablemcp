.PHONY: fmt test test-pg race build run-server run-executor run-scheduler compose-up compose-down compose-config verify

fmt:
	go fmt ./...

test:
	go test -short ./...

# Full PostgreSQL-backed suite (boots a throwaway container via testcontainers,
# or reuses DURABLEMCP_TEST_DATABASE_URL when set).
test-pg:
	go test ./... -count=1

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

# Local equivalent of the core CI verification. Note: no `go fmt` here — this
# checkout uses CRLF, so repo-wide reformatting would produce pure-noise diffs.
verify:
	go build ./...
	go vet ./...
	go test ./... -count=1
