.PHONY: tidy build test vet run-dev test-integration up down

tidy:  ## génère go.sum (une fois, avec accès au réseau) : à versionner
	go mod tidy

build:
	CGO_ENABLED=0 go build -trimpath -o bin/server ./cmd/server

vet:
	go vet ./...

test: vet
	go test -race -count=1 ./...

# Développement rapide sans base de données (données perdues à l'arrêt).
# Les identifiants ci-dessous sont réservés au développement local.
run-dev:
	STORE=memory ADDR=127.0.0.1:8080 \
	JWT_SECRET=dev-only-secret-change-me-0123456789abcdef \
	ADMIN_EMAIL=admin@setram.test ADMIN_PASSWORD=dev-admin-password \
	SEED_DEMO=true DEMO_PASSWORD=dev-demo-password \
	go run ./cmd/server

# Tests de la couche PostgreSQL. Le nom de la base doit contenir « test » (le schéma est réinitialisé).
#   make test-integration TEST_DATABASE_URL=postgres://user:pass@localhost:5432/helpdesk_test?sslmode=disable
test-integration:
	go test -tags=integration -count=1 ./internal/store/postgres/...

up:
	docker compose up --build -d

down:
	docker compose down
