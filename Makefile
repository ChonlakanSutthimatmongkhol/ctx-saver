BINARY_NAME := ctx-saver
BIN_DIR     := bin
INSTALL_DIR := /usr/local/bin
CMD_PKG     := ./cmd/$(BINARY_NAME)

.PHONY: build test install lint clean

## build: compile a single static binary to bin/ctx-saver
build:
	CGO_ENABLED=0 go build -ldflags "-s -w" -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_PKG)

## test: run all unit tests with race detector and coverage
test:
	go test -race -coverprofile=coverage.out $$(go list ./internal/... | grep -v /server)
	go tool cover -func=coverage.out

## install: build then copy binary to /usr/local/bin
install: build
	cp $(BIN_DIR)/$(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_NAME)
	@echo "Installed to $(INSTALL_DIR)/$(BINARY_NAME)"
	@echo ""
	@echo "Claude Code:  claude mcp add ctx-saver -- $(INSTALL_DIR)/$(BINARY_NAME)"
	@echo "VS Code:      add to .vscode/mcp.json → see configs/vscode-copilot/README.md"

## lint: run golangci-lint (must be installed separately)
lint:
	golangci-lint run ./...

## clean: remove build artefacts
clean:
	rm -rf $(BIN_DIR) coverage.out

.DEFAULT_GOAL := build
