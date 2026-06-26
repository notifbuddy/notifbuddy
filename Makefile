.PHONY: help generate gen-go gen-ts dev dev-backend dev-frontend build build-backend build-frontend install clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

## ---- Code generation (from spec/openapi.yaml) ----

generate: gen-go gen-ts ## Regenerate both server stub and TS client from the spec

gen-go: ## Generate the Go server (ogen) from the spec
	cd backend && go generate ./...

gen-ts: ## Generate the TypeScript client types from the spec
	cd frontend && npm run generate

## ---- Install ----

install: ## Install all dependencies (Go modules + npm)
	cd backend && go mod download
	cd frontend && npm install

## ---- Dev ----

dev-backend: ## Run the Go API server on :8080
	cd backend && go run .

dev-frontend: ## Run the SvelteKit dev server on :5173
	cd frontend && npm run dev

## ---- Build ----

build: build-backend build-frontend ## Build backend binary and frontend static site

build-backend: ## Compile the Go server to backend/bin/server
	cd backend && go build -o bin/server .

build-frontend: ## Build the SvelteKit SPA to frontend/build
	cd frontend && npm run build

## ---- Misc ----

clean: ## Remove build artifacts
	rm -rf backend/bin frontend/build frontend/.svelte-kit
