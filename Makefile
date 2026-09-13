GO ?= go
NPM ?= npm
export GOWORK := off

.PHONY: build build-cli build-web test check fmt preview clean install-duckdb test-web

install-duckdb:
	sh scripts/install-duckdb.sh

build: build-cli build-web

build-cli:
	$(GO) build -trimpath -o bin/copernicus ./cmd/copernicus

build-web:
	cd web && $(NPM) run build

test:
	$(GO) test ./cmd/... ./internal/... ./compatibility/...

test-web: build
	cd web && $(NPM) test

check:
	@test -z "$$(gofmt -l cmd internal compatibility)" || { gofmt -l cmd internal compatibility; exit 1; }
	$(MAKE) test
	$(GO) vet ./cmd/... ./internal/... ./compatibility/...
	cd web && $(NPM) run check
	$(MAKE) build

fmt:
	gofmt -w cmd internal compatibility
	cd web && $(NPM) run format

preview: build-web
	cd web && $(NPM) run preview

clean:
	rm -f bin/copernicus
	rm -rf web/dist
