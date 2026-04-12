.PHONY: install lint test

install: ## Install dependencies (uv sync)
	uv sync --all-groups

lint: ## Lint with ruff
	uv run ruff check .

test: ## Run tests with pytest
	uv run pytest -v
