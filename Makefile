.PHONY: build test lint run docker-build clean coverage

# Build version from git tag or commit; fall back to "dev".
BUILD_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

build:
	CGO_ENABLED=0 go build -ldflags="-s -w -X github.com/hanmahong5-arch/lurus-tally/internal/pkg/version.Version=$(BUILD_VERSION)" -trimpath -o tally-backend ./cmd/server

test:
	go test -count=1 ./...

lint:
	golangci-lint run ./...

run:
	go run ./cmd/server

docker-build:
	docker build --build-arg BUILD_VERSION=$(BUILD_VERSION) -t lurus-tally:local .

clean:
	rm -f tally-backend coverage.out

coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out
