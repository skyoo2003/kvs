.PHONY: all setup clean build test lint

all: lint test build

setup:
	pipenv sync -d
	pipenv run pre-commit install

clean:
	rm -rf dist/

build:
	go build -o dist/kvs

test:
	go test -v ./...

lint:
	golangci-lint run ./...
