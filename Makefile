# Resolve Go from PATH, falling back to the standard install location.
GO ?= $(shell command -v go 2>/dev/null || echo /usr/local/go/bin/go)

APP_NAME := taskmanager
BIN_DIR  := bin
GOOS     ?= linux
GOARCH   ?= amd64
IMAGE    ?= taskmanager:latest
IMAGE_DEV   ?= taskmanager:dev
PORT     ?= 8080
DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/tasks?sslmode=disable

.PHONY: help run build test fmt vet tidy clean docker-build docker-dev-build docker-up docker-down k8s-image k8s-up k8s-down k8s-forward

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

run: ## Run the server locally
	PORT=$(PORT) DATABASE_URL=$(DATABASE_URL) $(GO) run ./cmd/server

build: ## Build a static Linux binary (CGO disabled)
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -trimpath -ldflags="-s -w" -o $(BIN_DIR)/$(APP_NAME) ./cmd/server

test: ## Run all tests
	$(GO) test ./...

fmt: ## Format Go source
	$(GO) fmt ./...

vet: ## Run go vet
	$(GO) vet ./...

tidy: ## Tidy go.mod/go.sum
	$(GO) mod tidy

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)

docker-build: ## Build the production Docker image
	docker build -t $(IMAGE) .

docker-dev-build: ## Build the development image
	docker build --target dev -t $(IMAGE_DEV) .

docker-up: ## Start app + Postgres with Docker Compose
	docker compose up --build -d

docker-down: ## Stop Docker Compose services
	docker compose down

k8s-image: docker-build ## Load the image into Minikube
	minikube image load $(IMAGE)

k8s-up: k8s-image ## Deploy to Minikube
	kubectl apply -k k8s/
	kubectl -n taskmanager rollout status statefulset/postgres --timeout=180s
	kubectl -n taskmanager rollout status deployment/taskmanager --timeout=180s

k8s-down: ## Delete the Kubernetes resources
	kubectl delete -k k8s/ --ignore-not-found

k8s-forward: ## Forward the app service to localhost:8081
	kubectl -n taskmanager port-forward svc/taskmanager 8081:80
