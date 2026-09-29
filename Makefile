.PHONY: all setup clean build test lint lint-fix vet coverage race soak bench memtier notice licenses verify-release-artifacts

# How long `make soak` runs for. The full run behind the numbers in the docs is SOAK=4h.
SOAK ?= 5m

# How long a stopped node stays down before the cluster soak restarts it. SOAK_DOWN=0 brings it
# back instantly, which is the crash-loop condition the heap numbers in the docs were taken under.
SOAK_DOWN ?= 10s

# How many times `make bench` repeats each benchmark. Comparing two builds with benchstat wants
# ten or so; one is enough to see a number.
BENCH_COUNT ?= 1

# What `make memtier` starts: memory, durable, or cluster. The performance page has all three.
BENCH_MODE ?= durable

# How many seconds `make memtier` keeps the load on.
BENCH_TIME ?= 60

all: vet lint test build

notice:
	@./scripts/verify-notice.sh

licenses:
	@rm -rf dist/THIRD_PARTY_LICENSES
	@./scripts/collect-third-party-licenses.sh dist/THIRD_PARTY_LICENSES

verify-release-artifacts:
	@./scripts/verify-release-artifacts.sh dist

setup:
	@pre-commit install

clean:
	@rm -rf dist/

build:
	@go build -o dist/kvs ./cmd/kvs

test:
	@go test -v ./...

lint:
	@golangci-lint run ./...

lint-fix:
	@golangci-lint run --fix ./...

vet:
	@go vet ./...

coverage:
	@go test ./... -coverprofile=coverage.out -covermode=atomic
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

race:
	@go test -race ./...

# Load and fault injection over hours. Only the two packages naming -soak are listed, because
# passing an unknown flag to the others fails them before they start, and they have to come
# before -soak: go test treats everything after a flag it does not know as arguments for the
# test binary and falls back to the current directory. -timeout 0 leaves the deadline to the
# test, so raising SOAK never means recalculating a second number.
soak:
	@go test ./pkg/kvs ./internal/cluster -run TestSoak -soak $(SOAK) -soak-down $(SOAK_DOWN) -timeout 0 -v

# Go benchmarks for the store, the RESP path, and a three-node cluster. -run '^$' skips the
# tests in the same packages, which would otherwise run first and cost more than the benchmarks.
bench:
	@go test ./pkg/kvs ./internal/server ./internal/cluster -run '^$$' -bench . -benchmem -count $(BENCH_COUNT)

# Real load from memtier_benchmark against a kvs built from this tree. Needs memtier_benchmark
# and redis-cli on PATH.
memtier: build
	@./scripts/memtier.sh $(BENCH_MODE) $(BENCH_TIME)
