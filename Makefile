GO ?= go
VERSION ?= $(shell cat internal/buildinfo/VERSION)
PREFIX ?= $(HOME)/.local
LDFLAGS = -s -w -X main.version=$(VERSION)

.PHONY: build run test check smoke install uninstall snapshot clean
build:
	@mkdir -p bin
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/metra ./cmd/metra
run: build
	./bin/metra
test:
	$(GO) test -race -timeout 90s ./...
check:
	$(GO) vet ./...
	$(GO) test -race -timeout 90s ./...
smoke: build
	python3 scripts/smoke.py bin/metra
install: build
	install -d "$(DESTDIR)$(PREFIX)/bin"
	install -m 755 bin/metra "$(DESTDIR)$(PREFIX)/bin/metra"
uninstall:
	rm -f "$(DESTDIR)$(PREFIX)/bin/metra"
snapshot:
	goreleaser release --snapshot --clean --skip=publish
	python3 scripts/homebrew.py dist
clean:
	rm -rf bin dist coverage.out
