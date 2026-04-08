BINARY_NAME=slackers
BUILD_DIR=build
VERSION=0.20.0
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: all build clean install uninstall test lint run setup help

all: build

build:
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/slackers

run: build
	$(BUILD_DIR)/$(BINARY_NAME)

install:
	bash scripts/install.sh

uninstall:
	bash scripts/uninstall.sh

cleanup:
	bash scripts/cleanup.sh

setup: build
	$(BUILD_DIR)/$(BINARY_NAME) setup

test:
	go test ./...

lint:
	go vet ./...

clean:
	rm -rf $(BUILD_DIR)
	go clean

# Cross-compilation targets
build-linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/slackers

build-darwin:
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/slackers

build-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/slackers

build-all: build-linux build-darwin build-windows

help:
	@echo "Usage:"
	@echo "  make build      - Build the binary"
	@echo "  make run        - Build and run"
	@echo "  make install    - Install to ~/.local/bin"
	@echo "  make uninstall  - Remove installation"
	@echo "  make cleanup    - Clean local data"
	@echo "  make setup      - Run interactive setup"
	@echo "  make test       - Run tests"
	@echo "  make lint       - Run linter"
	@echo "  make clean      - Remove build artifacts"
	@echo "  make build-all  - Cross-compile for all platforms"
