.PHONY: build install test clean lint check-mod vuln check-docs check-docs-sync check-i18n check-guide-i18n check-schema-sync package-macos dashboard-binaries dashboard-release

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

check-mod:
	go mod tidy
	@git diff --exit-code go.mod go.sum || (echo "go.mod/go.sum 未同步，請執行 go mod tidy" && exit 1)

vuln:
	govulncheck ./...

check-docs:
	@bash scripts/check-docs.sh

check-docs-sync:
	@bash scripts/check-docs-sync.sh $(BASE)

check-i18n:
	@bash scripts/check-i18n.sh

check-guide-i18n:
	@bash scripts/check-guide-i18n.sh

check-schema-sync:
	go test ./internal/schemasync/...

package-macos:
	@bash scripts/package-macos.sh

# 交叉編譯 macOS app 打包所需的 darwin universal binary（arm64 + amd64）
dashboard-binaries:
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "-s -w -X main.version=$(VERSION)" -o bin/4x-darwin-arm64 ./cmd/4x
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "-s -w -X main.version=$(VERSION)" -o bin/4x-darwin-amd64 ./cmd/4x

# 一鍵發布：build → sign（Developer ID）→ notarize → staple
# 需先在 .env 或環境變數設定 CODESIGN_IDENTITY 及公證憑證，設定方式見 docs/guide/dashboard.md
dashboard-release: dashboard-binaries
	@bash scripts/package-macos.sh
	@bash scripts/notarize-macos.sh
