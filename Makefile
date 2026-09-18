GOLANGCI_VERSION := v2.13.2
GOLANGCI := $(CURDIR)/.tools/golangci-lint-$(GOLANGCI_VERSION)

.PHONY: build test check tools fmt fmt-check lint
build:
	@mkdir -p bin
	CGO_ENABLED=0 go build -o bin/csquad.install ./cmd/csquad
	mv bin/csquad.install bin/csquad

test:
	go test -race ./...

tools: $(GOLANGCI)

$(GOLANGCI):
	@mkdir -p .tools
	GOBIN=$(CURDIR)/.tools go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
	mv .tools/golangci-lint $(GOLANGCI)

fmt: tools
	$(GOLANGCI) fmt

fmt-check: tools
	@diff=$$(mktemp); trap 'rm -f "$$diff"' EXIT HUP INT TERM; \
	status=0; $(GOLANGCI) fmt --diff > "$$diff" || status=$$?; \
	if test "$$status" -ne 0 || test -s "$$diff"; then cat "$$diff"; echo 'Run make fmt to format Go sources.' >&2; exit 1; fi

lint: tools
	$(GOLANGCI) run

check: fmt-check lint test

PREFIX ?= $(HOME)/.local
.PHONY: install
install: build
	bin/csquad doctor --strict
	install -d "$(PREFIX)/bin"
	install -m 755 bin/csquad "$(PREFIX)/bin/csquad.install"
	mv "$(PREFIX)/bin/csquad.install" "$(PREFIX)/bin/csquad"
	@echo 'Installed csquad. Ensure $(PREFIX)/bin is on PATH; see csquad completion --help.'

GORELEASER_VERSION := v2.18.2
GORELEASER := $(CURDIR)/.tools/goreleaser-$(GORELEASER_VERSION)
.PHONY: package-check snapshot completions
$(GORELEASER):
	@mkdir -p .tools
	GOBIN=$(CURDIR)/.tools go install github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION)
	mv .tools/goreleaser $(GORELEASER)

package-check: $(GORELEASER)
	$(GORELEASER) check

completions:
	@mkdir -p dist/completions
	go run ./cmd/csquad completion bash > dist/completions/csquad.bash
	go run ./cmd/csquad completion zsh > dist/completions/_csquad
	go run ./cmd/csquad completion fish > dist/completions/csquad.fish

snapshot: package-check
	bash scripts/snapshot.sh "$(GORELEASER)"

.PHONY: test-packaging
test-packaging:
	python3 scripts/test-packaging.py
