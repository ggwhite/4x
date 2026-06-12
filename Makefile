.PHONY: build install test clean check-docs check-i18n

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/4x ./cmd/4x

install:
	go install -ldflags "-X main.version=$(VERSION)" ./cmd/4x

test:
	go test ./...

clean:
	rm -rf bin/

lint:
	go vet ./...

check-docs:
	@bash scripts/check-docs.sh

check-i18n:
	@bash scripts/check-i18n.sh
