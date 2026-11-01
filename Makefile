.PHONY: build test vet web-test dev

build: admin-ui-embed
	go build -o bin/ ./cmd/...

test: vet web-test admin-ui-test
	go test -race ./...

vet: admin-ui-embed
	go vet ./...

web-test:
	node --test internal/admin/webtest/lib.test.mjs

# loginhttp + gateway + admin in one terminal
dev: admin-ui-embed
	go run ./cmd/devall

# Geeker-Admin source and reproducible production output for Go embed
.PHONY: admin-ui-install admin-ui-dev admin-ui-check admin-ui-build admin-ui-preview admin-api-sandbox
admin-ui-install:
	cd web/admin && corepack pnpm install --frozen-lockfile

admin-ui-dev:
	cd web/admin && corepack pnpm dev

admin-ui-check:
	cd web/admin && corepack pnpm typecheck

admin-ui-build: admin-ui-install
	cd web/admin && corepack pnpm build

admin-ui-preview:
	cd web/admin && corepack pnpm preview

admin-api-sandbox: admin-ui-embed
	bash tools/admin-api-sandbox.sh

.PHONY: admin-ui-test
admin-ui-test: admin-ui-install
	cd web/admin && corepack pnpm test

.PHONY: admin-ui-embed admin-live-sandbox
admin-ui-embed: admin-ui-build
	node tools/package-admin-ui.mjs

admin-live-sandbox: build
	bash tools/admin-live-sandbox.sh
