.PHONY: dev test lint build agent web

COMPOSE := docker compose -f deploy/docker-compose.yml

web:
	cd web && npm ci && npm run build

dev:
	$(COMPOSE) up --build

test:
	go test ./... -race
	cd web && npm ci && npm test

lint:
	golangci-lint run
	cd web && npm ci && npm run check

build: web
	go build -o bin/fleetd ./cmd/fleetd
	./scripts/check-bundle-size.sh

agent:
	@echo "agent build arrives in task E2" && exit 1
