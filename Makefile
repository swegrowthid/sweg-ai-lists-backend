.PHONY: run build build-linux migrate-up migrate-down migrate-status vet test

run:
	go run ./cmd/api

build:
	mkdir -p dist/bin
	go build -o dist/bin/api ./cmd/api
	go build -o dist/bin/migrate ./cmd/migrate

build-linux:
	mkdir -p dist/bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -trimpath -ldflags "-s -w" \
		-o dist/bin/api ./cmd/api
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -trimpath -ldflags "-s -w" \
		-o dist/bin/migrate ./cmd/migrate

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
