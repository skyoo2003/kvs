# KVS
A simple key-value store

[![CI](https://github.com/skyoo2003/kvs/workflows/CI/badge.svg)](https://github.com/skyoo2003/kvs/actions?query=workflow%3ACI) [![Go Reference](https://pkg.go.dev/badge/github.com/skyoo2003/kvs.svg)](https://pkg.go.dev/github.com/skyoo2003/kvs) [![Go Report Card](https://goreportcard.com/badge/github.com/skyoo2003/kvs)](https://goreportcard.com/report/github.com/skyoo2003/kvs)

## Installation

```sh
$ go install github.com/skyoo2003/kvs/cmd/kvs@latest
```

## Documentation

Import `github.com/skyoo2003/kvs` when you want a small in-memory store inside a Go program.

```go
package main

import (
	"fmt"

	"github.com/skyoo2003/kvs"
)

func main() {
	store := kvs.NewStore()
	_ = store.Put("language", "go")

	value, _ := store.Get("language")
	fmt.Println(value)
}
```

## Contributing

See `CONTRIBUTING.md` for the project contribution process.

## [License](LICENSE)

The MIT License

Copyright (c) 2020-2021 Sung-Kyu Yoo
