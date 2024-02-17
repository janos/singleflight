.DEFAULT_GOAL := help

GO_BIN ?= $(shell go env GOPATH)/bin

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

$(GO_BIN)/golangci-lint:
	@echo "==> Installing golangci-lint within "${GO_BIN}""
	@go install -v github.com/golangci/golangci-lint/cmd/golangci-lint@latest

.PHONY: lint
lint: $(GO_BIN)/golangci-lint ## Lint Go source files
	@echo "==> Linting Go source files"
	@golangci-lint run -v --fix -c .golangci.yaml ./...

.PHONY: build
build: ## Build
	go build -ldflags "-s -w" ./...

.PHONY: test
test: ## Run unit tests
	go test -v -race -count=1 ./...