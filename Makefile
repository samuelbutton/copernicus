GO ?= go
NPM ?= npm
PYTHON ?= python3
YAMATA_SOURCE ?= ../yamata
MODE ?= standalone
PORT ?= 8080
export PYTHONDONTWRITEBYTECODE := 1
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

.PHONY: demo demo-pair demo-serve demo-clean verify check-docs

demo: build
	$(PYTHON) scripts/walkthrough.py standalone

demo-pair: build
	$(PYTHON) scripts/walkthrough.py pair --yamata-source "$(YAMATA_SOURCE)" --go "$(GO)"

demo-serve:
	$(PYTHON) scripts/walkthrough.py serve --review "$(MODE)" --port "$(PORT)"

demo-clean:
	$(PYTHON) scripts/walkthrough.py clean

check-docs:
	$(PYTHON) scripts/check-docs.py

verify: check check-docs
	$(GO) mod verify
	$(GO) test -race -count=1 ./cmd/... ./internal/... ./compatibility/...
	$(PYTHON) tests/walkthrough.py --yamata-source "$(YAMATA_SOURCE)" --go "$(GO)" --npm "$(NPM)"
