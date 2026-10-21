BIN_DIR := bin
BINARY := $(BIN_DIR)/media-organizer
CMD := ./cmd/media-organizer

.PHONY: all build test vet lint sec vulncheck clean

all: vet test build

build:
	@mkdir -p $(BIN_DIR)
	go build -trimpath -o $(BINARY) $(CMD)

test:
	go test -v -race ./...

vet:
	go vet ./...

lint:
	@if command -v golangci-lint > /dev/null 2>&1; then golangci-lint run ./...; else echo "golangci-lint not installed; skipping lint"; fi

sec:
	go tool gosec ./...

vulncheck:
	go tool govulncheck ./...

clean:
	rm -rf $(BIN_DIR)
