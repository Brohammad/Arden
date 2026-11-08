.PHONY: up migrate run test build

build:
	go build -o bin/arden ./cmd/arden

DATABASE_URL ?= postgres://arden:arden@localhost:5432/arden?sslmode=disable
export DATABASE_URL

up:
	docker compose -f deploy/docker-compose.yml up -d postgres

migrate:
	go run ./cmd/arden migrate up

run:
	go run ./cmd/arden api

test:
	go test ./...
