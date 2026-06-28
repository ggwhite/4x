.PHONY: build install test clean lint check-docs check-docs-sync check-i18n package-macos

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/4x ./cmd/4x

install:
	go install -ldflags "-X main.version=$(VERSION)" ./cmd/4x

test:
	go test -race ./...

clean:
	rm -rf bin/

lint:
	go vet ./...
	@test -z "$$(git ls-files '*.go' | xargs gofmt -l)" || (echo "gofmt: 以下檔案格式不符，請執行 gofmt -w ." && git ls-files '*.go' | xargs gofmt -l && exit 1)
	@if command -v golangci-lint > /dev/null 2>&1; then golangci-lint run ./...; else echo "golangci-lint not installed, skipping"; fi

check-docs:
	@bash scripts/check-docs.sh

check-docs-sync:
	@bash scripts/check-docs-sync.sh $(BASE)

check-i18n:
	@bash scripts/check-i18n.sh

package-macos:
	@bash scripts/package-macos.sh
