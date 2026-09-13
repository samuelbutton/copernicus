GO ?= go
NPM ?= npm
export GOWORK := off

.PHONY: build build-cli build-web test check fmt preview clean

build: build-cli build-web

build-cli:
	$(GO) build -trimpath -o bin/copernicus ./cmd/copernicus

build-web:
	cd web && $(NPM) run build

test:
	$(GO) test ./cmd/... ./internal/...

check:
	@test -z "$$(gofmt -l cmd internal)" || { gofmt -l cmd internal; exit 1; }
	$(MAKE) test
	$(GO) vet ./cmd/... ./internal/...
	cd web && $(NPM) run check
	$(MAKE) build

fmt:
	gofmt -w cmd internal
	cd web && $(NPM) run format

preview: build-web
	cd web && $(NPM) run preview

clean:
	rm -f bin/copernicus
	rm -rf web/dist
