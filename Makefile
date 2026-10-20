BINARY := bin/flixflox-bot
PKG    := ./cmd/flixflox-bot

.PHONY: help build run test cover fmt vet tidy clean up down logs

help: ## Lista los targets disponibles
	@grep -E '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "  \033[36m%-8s\033[0m %s\n", $$1, $$2}'

build: ## Compila el bot en bin/
	go build -o $(BINARY) $(PKG)

run: ## Corre el bot leyendo .env
	set -a && . ./.env && set +a && go run $(PKG)

test: ## Corre los tests con el detector de carreras
	go test -race ./...

cover: ## Corre los tests y abre el reporte de cobertura
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

fmt: ## Formatea el código
	go fmt ./...

vet: ## Análisis estático
	go vet ./...

tidy: ## Sincroniza go.mod / go.sum
	go mod tidy

clean: ## Borra binarios y reportes
	rm -rf bin coverage.out

up: ## Levanta el Bot API local
	docker compose up -d

down: ## Baja el Bot API local
	docker compose down

logs: ## Sigue los logs del Bot API local
	docker compose logs -f
