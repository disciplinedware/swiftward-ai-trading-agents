.PHONY: help build up down restart logs shell migrate seed console

DC = docker compose --env-file ../../../.env

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

build: ## Build docker images
	$(DC) build

up: ## Start the stack in background
	$(DC) up -d

down: ## Stop and remove containers
	$(DC) down

restart: ## Restart all services
	$(DC) restart

logs: ## Tail logs from all services
	$(DC) logs -f

shell: ## Open a bash shell in the web container
	$(DC) exec web bash

migrate: ## Run database migrations
	$(DC) exec web bin/rails db:migrate

seed: ## Seed the database
	$(DC) exec web bin/rails db:seed

console: ## Open Rails console
	$(DC) exec web bin/rails console
