# ferry's developer entry point.
#
# Everything CI runs, a developer can run here with the same flags, from the
# same pinned tool versions. The lint and format jobs in .github/workflows/ci.yml
# call these targets rather than repeating the commands, so the two cannot drift.

PKG := github.com/onhotpath/ferry

PROJECT_DIR := $(patsubst %/,%,$(dir $(abspath $(lastword $(MAKEFILE_LIST)))))
BIN_DIR := $(PROJECT_DIR)/.bin

# Every module in the workspace, core first.
#
# Adding a module is two edits: a `use` line in go.work, and an entry here.
# CI does not read this list - it globs driver/*/go.mod and examples/go.mod and
# derives its matrix from that, so a module added under driver/ is tested
# whether or not anyone remembers this file. The two are checked against each
# other by the "go.work uses every discovered module" step, which fails when
# they disagree.
MODULES := . driver/env driver/http driver/kv driver/yaml examples

# Pinned, not `latest`: a lint failure should always be this repository's doing
# and never a tool release nobody asked for. This file is the only place either
# version is named; the CI lint job runs `make lint` rather than repeating them.
GOLANGCI_LINT_VERSION ?= v2.12.2
GCI_VERSION ?= v0.14.0

GOLANGCI_LINT := $(BIN_DIR)/golangci-lint
GCI := $(BIN_DIR)/gci

# Whatever toolchain the workspace resolves, which go.work pins to go1.27rc2.
GOVERSION := $(shell go env GOVERSION)

# Tools install into .bin and never into the user's $GOPATH/bin, so building
# ferry cannot change the version of a tool used on some other project.
GOINSTALL := GOBIN=$(BIN_DIR) go install

# The import grouping gci enforces, identical to the gci section list in
# .golangci.yml. Both are run; this one can also rewrite.
GCI_SECTIONS := -s standard -s default -s "prefix($(PKG))"

.DEFAULT_GOAL := help

## ---- formatting ------------------------------------------------------------

.PHONY: fmt
fmt: ## Rewrite every Go file with gofmt.
	@echo "==> gofmt -w"
	@gofmt -l -w $(PROJECT_DIR)

.PHONY: imports
imports: $(GCI) ## Rewrite import blocks into stdlib / external / ferry groups.
	@echo "==> gci write"
	@$(GCI) write --skip-generated $(GCI_SECTIONS) $(PROJECT_DIR)

.PHONY: imports-check
imports-check: $(GCI) ## Fail if any import block is out of group order.
	@echo "==> gci diff"
	@out=$$($(GCI) diff --skip-generated $(GCI_SECTIONS) $(PROJECT_DIR)); \
	if [ -n "$$out" ]; then \
		echo "FAIL: these import blocks are not in gci order:"; \
		echo "$$out"; \
		echo "      run 'make imports'."; \
		exit 1; \
	fi
	@echo "OK: import groups are in order."

## ---- checks ----------------------------------------------------------------

.PHONY: vet
vet: ## go vet every module.
	@fail=0; for m in $(MODULES); do \
		echo "==> go vet $$m"; \
		( cd "$(PROJECT_DIR)/$$m" && go vet ./... ) || fail=1; \
	done; \
	exit $$fail

.PHONY: check
check: vet imports-check ## go vet, assert gofmt cleanliness, assert import order.
	@echo "==> gofmt -l"
	@out=$$(gofmt -l $(PROJECT_DIR)); \
	if [ -n "$$out" ]; then \
		echo "FAIL: these files are not gofmt'd:"; \
		printf '  %s\n' $$out; \
		gofmt -d $$out; \
		exit 1; \
	fi
	@echo "OK: gofmt -l printed nothing."

.PHONY: lint
lint: lint-canary ## golangci-lint every module, after verifying the config.
	@echo "==> golangci-lint config verify"
	@$(GOLANGCI_LINT) config verify
	@fail=0; for m in $(MODULES); do \
		echo "==> golangci-lint run $$m"; \
		( cd "$(PROJECT_DIR)/$$m" && $(GOLANGCI_LINT) run ./... ) || fail=1; \
	done; \
	exit $$fail

# The linter set is only worth what it actually reports, and "0 issues" is the
# same output whether a linter ran and found nothing or never ran at all (#271).
#
# unused is the one that needs saying out loud. golangci-lint hosts it inside
# the shared goanalysis_metalinter runner alongside staticcheck, so a bundled
# analyser that cannot read the toolchain's source takes unused down with it -
# which is exactly why staticcheck is switched off in .golangci.yml today. That
# pin is moved by Renovate, not by anyone who has read the comment there, so the
# claim "unused is unaffected" is asserted here rather than believed.
#
# lintcanary.go carries a build tag no ordinary run sets, so the dead function
# it holds is invisible to build, vet, test and `make lint` itself.
CANARY_TAG := ferrylintcanary
CANARY_FUNC := deadOnPurpose

.PHONY: lint-canary
lint-canary: $(GOLANGCI_LINT) ## Assert the unused linter still reports dead code.
	@echo "==> golangci-lint canary (unused)"
	@out=$$(cd $(PROJECT_DIR) && $(GOLANGCI_LINT) run \
		--build-tags=$(CANARY_TAG) --enable-only=unused ./... 2>&1); \
	case "$$out" in \
		*"func $(CANARY_FUNC) is unused"*) ;; \
		*) \
			echo "$$out"; \
			echo "FAIL: unused did not report lintcanary.go's $(CANARY_FUNC)."; \
			echo "      Either the linter is no longer running - a golangci-lint"; \
			echo "      bump can take it down with the analyser it shares a"; \
			echo "      runner with - or the canary file was edited. Do not"; \
			echo "      silence this; find out which."; \
			exit 1; \
	esac
	@echo "OK: unused reported the canary, so it is running."

## ---- tests -----------------------------------------------------------------

.PHONY: test
test: ## go test every module.
	@fail=0; for m in $(MODULES); do \
		echo "==> go test $$m"; \
		( cd "$(PROJECT_DIR)/$$m" && go test ./... ) || fail=1; \
	done; \
	exit $$fail

.PHONY: test-race
test-race: ## go test -race every module.
	@fail=0; for m in $(MODULES); do \
		echo "==> go test -race $$m"; \
		( cd "$(PROJECT_DIR)/$$m" && go test -race ./... ) || fail=1; \
	done; \
	exit $$fail

.PHONY: cover
cover: ## Coverage profile for core, and the per-function report.
	@# Core only, which is the same scope CI gates: core is pure logic with no
	@# I/O to plead, and a driver's coverage is a driver's business.
	@echo "==> go test -coverprofile (core)"
	@cd $(PROJECT_DIR) && go test -covermode=atomic -coverprofile=cover.out ./...
	@cd $(PROJECT_DIR) && go tool cover -func=cover.out

.PHONY: nojsonv2
nojsonv2: ## Assert core still builds and tests with encoding/json/v2 excluded.
	@# ADR-0002 bars encoding/json/v2 and encoding/json/jsontext from core, so
	@# that GOEXPERIMENT=nojsonv2 stays the consumer's switch to throw. CI
	@# asserts it on every push; this target is how it is checked before the
	@# push, since the import that breaks it is easy to add and invisible until
	@# somebody sets the variable.
	@echo "==> GOEXPERIMENT=nojsonv2 (core)"
	@cd $(PROJECT_DIR) && GOEXPERIMENT=nojsonv2 go build ./...
	@cd $(PROJECT_DIR) && GOEXPERIMENT=nojsonv2 go test ./...
	@echo "OK: core builds and tests with encoding/json/v2 excluded."

.PHONY: godoc-check
godoc-check: ## Assert no published doc comment cites an ADR or an issue number.
	@# godoc is written for the person using ferry, and a reader on pkg.go.dev
	@# has no ADR open and cannot resolve the reference. The reasoning lives in
	@# docs/adr/. Unexported and inline comments keep their citations, which is
	@# why this greps what `go doc` prints rather than the source: that is
	@# exactly the boundary. See CONTRIBUTING.md.
	@echo "==> go doc -all, every published package"
	@hits=$$(for m in $(MODULES); do \
		(cd $(PROJECT_DIR)/$$m && go list ./... | grep -v '/internal/'); \
	done | xargs -n1 go doc -all 2>/dev/null | grep -nE 'ADR-[0-9]|\(#[0-9]+\)' || true); \
	if [ -n "$$hits" ]; then \
		echo "$$hits"; \
		echo "FAIL: a published doc comment cites a design record. See CONTRIBUTING.md."; \
		exit 1; \
	fi
	@echo "OK: no published doc comment cites an ADR or an issue."

## ---- housekeeping ----------------------------------------------------------

.PHONY: tidy
tidy: ## go mod tidy every module.
	@fail=0; for m in $(MODULES); do \
		echo "==> go mod tidy $$m"; \
		( cd "$(PROJECT_DIR)/$$m" && go mod tidy ) || fail=1; \
	done; \
	exit $$fail

.PHONY: tools
tools: $(GOLANGCI_LINT) $(GCI) ## Install the pinned tools into .bin.

$(GOLANGCI_LINT):
	@echo "==> installing golangci-lint $(GOLANGCI_LINT_VERSION)"
	@# Built from source with GOTOOLCHAIN forced to the workspace's toolchain.
	@# Every released golangci-lint binary is compiled with Go 1.26 and refuses
	@# a module targeting 1.27 outright:
	@#   "the Go language version (go1.26) used to build golangci-lint is lower
	@#    than the targeted Go version (1.27)"
	@# Building it with the toolchain go.work pins produces a binary that
	@# accepts ADR-0001's floor. Measured on v2.12.2 with go1.27rc2.
	@#
	@# TODO: drop the GOTOOLCHAIN prefix once a golangci-lint release is built
	@# with Go 1.27, which cannot happen before Go 1.27 is GA.
	@GOTOOLCHAIN=$(GOVERSION) $(GOINSTALL) github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@$(GOLANGCI_LINT) --version

$(GCI):
	@echo "==> installing gci $(GCI_VERSION)"
	@$(GOINSTALL) github.com/daixiang0/gci@$(GCI_VERSION)

.PHONY: clean
clean: ## Remove installed tools and test output.
	@echo "==> clean"
	@rm -rf $(BIN_DIR)
	@rm -f $(PROJECT_DIR)/cover.out $(PROJECT_DIR)/coverage.html

.PHONY: help
help: ## List the targets.
	@echo "ferry - $(PKG)"
	@echo
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
	@echo
	@echo "modules: $(MODULES)"
