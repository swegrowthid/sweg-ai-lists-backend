.PHONY: run migrate-up migrate-down migrate-status vet test

run:
	go run ./cmd/api

migrate-up:
	go run ./cmd/migrate up

migrate-down:
	go run ./cmd/migrate down

migrate-status:
	go run ./cmd/migrate status

vet:
	go vet ./...

test:
	go test ./...
