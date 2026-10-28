BINARY := bin/flixflox-bot
PKG    := ./cmd/flixflox-bot

.PHONY: help build run test cover fmt vet tidy clean up down logs

help: ## List the available targets
	@grep -E '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "  \033[36m%-8s\033[0m %s\n", $$1, $$2}'

build: ## Build the bot into bin/
	go build -o $(BINARY) $(PKG)

run: ## Run the bot reading .env
	set -a && . ./.env && set +a && go run $(PKG)

test: ## Run the tests with the race detector
	go test -race ./...

cover: ## Run the tests and open the coverage report
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

fmt: ## Format the code
	go fmt ./...

vet: ## Static analysis
	go vet ./...

tidy: ## Sync go.mod / go.sum
	go mod tidy

clean: ## Delete binaries and reports
	rm -rf bin coverage.out

up: ## Start the local Bot API
	docker compose up -d

down: ## Stop the local Bot API
	docker compose down

logs: ## Follow the local Bot API logs
	docker compose logs -f
