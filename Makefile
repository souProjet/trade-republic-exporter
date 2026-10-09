BINARY := tr-export

.PHONY: help build demo test race lint fmt vet tidy clean
.DEFAULT_GOAL := help

help: ## Show this help
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "  %-8s %s\n", $$1, $$2}'

build: ## Build the binary
	go build -trimpath -ldflags "-s -w" -o $(BINARY) ./cmd/tr-export

demo: ## Run the dashboard with sample data
	go run ./cmd/tr-export --demo

test: ## Run the tests
	go test ./...

race: ## Run the tests with the race detector
	go test -race ./...

lint: ## Run golangci-lint (go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest)
	golangci-lint run

fmt: ## Format the code
	gofmt -w .

vet: ## Run go vet
	go vet ./...

tidy: ## Tidy the module files
	go mod tidy

clean: ## Remove build and export artifacts
	rm -f $(BINARY)
	rm -rf dist
