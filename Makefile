.PHONY: help generate gen-go gen-ts dev dev-backend dev-frontend dev-landing dev-docs build build-backend build-frontend build-landing install test-e2e test-e2e-ui clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

## ---- Code generation (from spec/*.yaml) ----

generate: gen-go gen-ts ## Regenerate both server stubs and TS clients from the specs

gen-go: ## Generate the Go server (ogen) from the spec
	cd backend && go generate ./...

gen-ts: ## Generate the TypeScript client types from the spec
	cd frontend && npm run generate

## ---- Install ----

install: ## Install all dependencies (Go modules + npm)
	cd backend && go mod download
	cd frontend && npm install
	cd landing && npm install

## ---- Dev ----

dev-backend: ## Run the Go API server on :8080
	cd backend && go run ./cmd/server

dev-frontend: ## Run the SvelteKit dev server on :5173
	cd frontend && npm run dev

dev-landing: ## Run the marketing site dev server on :5174
	cd landing && npm run dev

dev-docs: ## Run the docs server
	cd userdocs && npm run dev

## ---- Build ----

build: build-backend build-frontend build-landing ## Build the backend binary and both static sites

build-backend: ## Compile the Go server to backend/bin/server
	cd backend && go build -o bin/server ./cmd/server

build-frontend: ## Build the SvelteKit SPA to frontend/build
	cd frontend && npm run build

build-landing: ## Build the marketing site to landing/build
	cd landing && npm run build

## ---- Test ----

test-e2e: ## Run the backend black-box e2e suite in docker compose
	cd backend/e2e && ./run.sh

test-e2e-ui: ## Run the dashboard (Playwright) e2e suite in docker compose
	cd backend/e2e && ./run-ui.sh

## ---- Misc ----

clean: ## Remove build artifacts
	rm -rf backend/bin frontend/build frontend/.svelte-kit landing/build landing/.svelte-kit
