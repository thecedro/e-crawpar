# e-crawpar — test automation targets / alvos de automação de testes
#
# Layer map / mapa das camadas:
#   test-unit         -> layer 1: pure business rules (/test/unit)
#   test-integration  -> layer 2: concurrent pipeline, no IMAP (/test/integration)
#   test-contract     -> layer 3: IMAPClient safety contract (/test/contract)
#   test-e2e          -> layer 4: full path vs in-memory IMAP server (/test/e2e, tag e2e)
#   test-bdd          -> layer 5: executable Portuguese specifications (/test/features)
#   test-mutation     -> layer 6: gremlins over the rule-dense packages
#   test-all          -> everything except mutation
#
# Coverage gate / meta de cobertura: every function in internal/core >= 85%.

GO ?= go
COVERAGE_MIN_CORE ?= 85

.PHONY: help tools tidy lint test-unit test-integration test-contract test-e2e \
        test-bdd test-mutation test-all cover-check clean

help: ## list targets / lista os alvos
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

tools: ## install dev tools / instala ferramentas de dev
	$(GO) run github.com/go-gremlins/gremlins/cmd/gremlins@latest --help >/dev/null 2>&1 || true

tidy:
	$(GO) mod tidy

lint:
	$(GO) vet ./...
	@out=$$($(GO) run mvdan.cc/gofumpt@latest -l . 2>/dev/null); \
	if [ -n "$$out" ]; then echo "not formatted:"; echo "$$out"; exit 1; fi || true

# --- Layer 1: unit -----------------------------------------------------------
test-unit: ## pure rules, with race detector + core coverage gate
	$(GO) test -count=1 -race -timeout 120s \
		-coverpkg=./internal/core -coverprofile=coverage.out ./ ./test/unit/
	$(MAKE) --no-print-directory cover-check COVFILE=coverage.out

# --- Layer 2: integration ----------------------------------------------------
test-integration: ## concurrent pipeline under -race, synthetic headers only
	$(GO) test -count=1 -race -timeout 300s ./test/integration/

# --- Layer 3: contract (merge-blocking safety net) ----------------------------
test-contract: ## read-only + ENVELOPE-only + batched pagination, fake AND real adapter
	$(GO) test -count=1 -race -timeout 120s ./test/contract/

# --- Layer 4: end-to-end ------------------------------------------------------
test-e2e: ## full binary pipeline vs in-memory IMAP server (build tag: e2e)
	$(GO) test -count=1 -race -tags e2e -timeout 300s ./test/e2e/

# --- Layer 5: BDD -------------------------------------------------------------
test-bdd: ## executable Gherkin specifications in Portuguese
	$(GO) test -count=1 -v ./test/features/steps/

# --- Layer 6: mutation --------------------------------------------------------
# --integration is REQUIRED here: the suites live in /test/* (external test
# packages), so gremlins must run the whole suite per mutant instead of only
# the mutated package's own tests.
# -E rules scope mutations to internal/core (the rule-dense package).
test-mutation: ## gremlins over internal/core via the full /test suite
	$(GO) run github.com/go-gremlins/gremlins/cmd/gremlins@latest unleash \
		--tags "" \
		--coverpkg ./internal/core \
		--integration \
		--threshold-efficacy 70 \
		--workers 4 \
		-E "^test/" \
		-E "^(main|setup)\.go$$" \
		-E "^internal/(imapx|report|apperr)/" ; \
	status=$$?; \
	echo ""; \
	echo "NOTE: review surviving mutants against README-TESTING.md before accepting."; \
	exit $$status

# --- Aggregate -----------------------------------------------------------------
test-all: test-unit test-integration test-contract test-bdd test-e2e ## every hermetic layer
	@echo ""
	@echo "ALL TEST LAYERS GREEN ✔"

cover-check: ## enforce minimum coverage on internal/core functions
	@if [ ! -f "$(COVFILE)" ]; then echo "coverage file $(COVFILE) missing"; exit 1; fi
	@core_min=$$( $(GO) tool cover -func=$(COVFILE) \
		| grep 'internal/core/' \
		| awk '{gsub("%", "", $$3); if (min == "" || $$3 < min) min = $$3} END {print min+0}' ); \
	total=$$( $(GO) tool cover -func=$(COVFILE) \
		| awk '/^total:/ {gsub("%", "", $$3); print $$3}' ); \
	echo "internal/core worst function: $${core_min}%  |  total statements: $${total}%"; \
	if awk -v c="$$core_min" -v m="$(COVERAGE_MIN_CORE)" 'BEGIN {exit !(c >= m)}'; then \
		echo "coverage gate OK (>= $(COVERAGE_MIN_CORE)% on internal/core)"; \
	else \
		echo "COVERAGE GATE FAILED: internal/core has a function below $(COVERAGE_MIN_CORE)%"; \
		exit 1; \
	fi

clean:
	rm -f coverage.out coverage-unit.out
