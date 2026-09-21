# FRIDAY

BINARY  := friday
PKG     := ./cmd/server
DEPLOY  := build

.PHONY: help
help: ## List the targets
	@grep -hE '^[a-z-]+:.*##' $(MAKEFILE_LIST) | sort | awk -F':.*##' '{printf "  %-16s %s\n", $$1, $$2}'

.PHONY: build
build: ## Build for this machine
	go build -o $(BINARY) $(PKG)

.PHONY: deploy-build
deploy-build: ## Build for the server (linux/arm64)
	@mkdir -p $(DEPLOY)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o $(DEPLOY)/$(BINARY) $(PKG)
	@echo "$(DEPLOY)/$(BINARY): $$(du -h $(DEPLOY)/$(BINARY) | cut -f1)"

.PHONY: deploy-build-amd64
deploy-build-amd64: ## Build for an x86 server
	@mkdir -p $(DEPLOY)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o $(DEPLOY)/$(BINARY)-amd64 $(PKG)

.PHONY: test
test: ## Run the tests
	go test ./... -race

.PHONY: check
check: ## Everything that must pass before committing
	gofmt -l .
	go vet ./...
	go test ./... -race

.PHONY: run
run: ## Run against the local config
	go run $(PKG)

.PHONY: clean
clean: ## Remove build output
	rm -rf $(DEPLOY) $(BINARY)
