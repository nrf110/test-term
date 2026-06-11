# tt — developer tasks

BINARY      := tt
PKG         := ./cmd/tt
BIN_DIR     := bin
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.0.0-dev")
LDFLAGS     := -X main.version=$(VERSION)

.PHONY: build test lint vet fmt run clean tidy

build:
	@mkdir -p $(BIN_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(PKG)

test:
	go test ./...

vet:
	go vet ./...

# golangci-lint is optional locally; CI installs it.
lint: vet
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || \
		echo "golangci-lint not installed; ran 'go vet' only"

fmt:
	gofmt -s -w .

run:
	go run $(PKG)

tidy:
	go mod tidy

clean:
	rm -rf $(BIN_DIR)
