.PHONY: all setup clean build test lint

all: lint test build

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
