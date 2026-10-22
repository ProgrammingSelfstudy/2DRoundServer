.PHONY: build test vet web-test dev

build:
	go build -o bin/ ./cmd/...

test: vet web-test
	go test -race ./...

vet:
	go vet ./...

web-test:
	node --test internal/admin/webtest/lib.test.mjs

# loginhttp + gateway + admin in one terminal
dev:
	go run ./cmd/devall
