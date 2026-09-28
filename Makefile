# voice-goio
#
# Everything here is CGO_ENABLED=0 on purpose: the Windows backends reach the
# platform through syscall, never through a C toolchain.

export CGO_ENABLED := 0

GO      ?= go
OUT     ?= bin
PKGS    := ./...

.PHONY: all
all: fmt vet test cross

.PHONY: fmt
fmt:
	gofmt -w $(shell find . -name '*.go' -not -path './.claude/*')

.PHONY: vet
vet:
	$(GO) vet $(PKGS)

.PHONY: test
test:
	$(GO) test -count=1 $(PKGS)

.PHONY: race
race:
	$(GO) test -race -count=1 $(PKGS)

# The Windows-only files never get to rot: they are compiled from macOS on
# every run, which is the whole point of the stub-first layout.
.PHONY: cross
cross:
	GOOS=windows GOARCH=amd64 $(GO) build $(PKGS)
	GOOS=darwin  GOARCH=arm64 $(GO) build $(PKGS)
	GOOS=linux   GOARCH=amd64 $(GO) build $(PKGS)
	GOOS=windows GOARCH=amd64 $(GO) build -tags commercial $(PKGS)

.PHONY: deps
deps:
	@grep -qE '^\s*require' go.mod && { echo "go.mod has requires"; exit 1; } || echo "go.mod: zero requires"
	@$(GO) list -deps $(PKGS) | grep -v '^github.com/mrlm-net/voice-goio' | awk -F/ '$$1 ~ /\./ { print "non-stdlib: " $$0; bad=1 } END { exit bad }' && echo "dependencies: stdlib only"

.PHONY: build
build:
	$(GO) build -o $(OUT)/ ./cmd/...

.PHONY: build-windows
build-windows:
	GOOS=windows GOARCH=amd64 $(GO) build -o $(OUT)/windows/ ./cmd/...

# --- things to listen to ----------------------------------------------------

.PHONY: demo
demo:
	$(GO) run ./cmd/demo -mode session

.PHONY: demo-arrival
demo-arrival:
	$(GO) run ./cmd/demo -mode arrival

.PHONY: demo-accents
demo-accents:
	$(GO) run ./cmd/demo -mode accents -n 8

.PHONY: demo-profiles
demo-profiles:
	$(GO) run ./cmd/demo -mode profiles

.PHONY: demo-live
demo-live:
	$(GO) run ./cmd/demo -mode live

.PHONY: wav
wav:
	$(GO) run ./cmd/voicecheck synth -out wav -limit 8

# --- quality bars -----------------------------------------------------------

.PHONY: bars
bars:
	$(GO) run ./cmd/voicecheck voices
	$(GO) run ./cmd/voicecheck recog -backend fake -min 95

# Build and test from a clean clone of HEAD.
#
# This is the check that a file exists in the repository rather than only on
# the developer's disk. An over-broad .gitignore pattern once excluded
# internal/wav from a release commit: everything built locally, and every CI
# job failed on a fresh checkout. Run this before tagging.
.PHONY: release-check
release-check:
	@set -e; \
	dirty="$$(git status --porcelain)"; \
	if [ -n "$$dirty" ]; then echo "working tree is dirty:"; echo "$$dirty"; exit 1; fi; \
	tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp"' EXIT; \
	git clone --quiet --no-local . "$$tmp/repo"; \
	cd "$$tmp/repo"; \
	echo "clean clone at $$(git rev-parse --short HEAD)"; \
	CGO_ENABLED=0 go build ./...; \
	CGO_ENABLED=0 go vet ./...; \
	CGO_ENABLED=0 go test -count=1 ./... > /dev/null; \
	for os in windows darwin linux; do GOOS=$$os CGO_ENABLED=0 go build ./...; done; \
	echo "release-check: clean clone builds, vets, tests and cross-compiles"

.PHONY: clean
clean:
	rm -rf $(OUT) wav wav-audit
