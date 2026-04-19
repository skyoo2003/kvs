.PHONY: all setup clean build test lint lint-fix vet coverage race

all: vet lint test build

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
