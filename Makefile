APP_NAME ?= gh-threads
CMD_PATH ?= ./cmd/gh-threads
DIST_DIR ?= dist

.PHONY: build test clean release release-darwin release-linux release-windows ensure-dist

build: ensure-dist ## Build the CLI for the current platform
	GO111MODULE=on go build -o $(DIST_DIR)/$(APP_NAME) $(CMD_PATH)

test: ## Run the Go test suite
	GO111MODULE=on go test ./...

clean: ## Remove built artifacts
	rm -rf $(DIST_DIR)

release: release-darwin release-linux release-windows ## Build release binaries for all supported targets

release-darwin: ensure-dist ## Build macOS binaries for arm64 and amd64
	GO111MODULE=on GOOS=darwin GOARCH=arm64 go build -o $(DIST_DIR)/$(APP_NAME)-darwin-arm64 $(CMD_PATH)
	GO111MODULE=on GOOS=darwin GOARCH=amd64 go build -o $(DIST_DIR)/$(APP_NAME)-darwin-amd64 $(CMD_PATH)

release-linux: ensure-dist ## Build Linux binaries for arm64 and amd64
	GO111MODULE=on GOOS=linux GOARCH=arm64 go build -o $(DIST_DIR)/$(APP_NAME)-linux-arm64 $(CMD_PATH)
	GO111MODULE=on GOOS=linux GOARCH=amd64 go build -o $(DIST_DIR)/$(APP_NAME)-linux-amd64 $(CMD_PATH)

release-windows: ensure-dist ## Build Windows binary for amd64
	GO111MODULE=on GOOS=windows GOARCH=amd64 go build -o $(DIST_DIR)/$(APP_NAME)-windows-amd64.exe $(CMD_PATH)

ensure-dist:
	mkdir -p $(DIST_DIR)
