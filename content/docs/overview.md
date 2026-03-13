---
title: "Overview"
weight: 1
---

KVS is a simple key-value store written in Go. It provides an in-memory store that can be used as a library within Go programs or via its command-line interface.

## Features

- In-memory key-value storage
- Clean Go API for programmatic use
- Cobra-based CLI for command-line operations
- Viper configuration support

## Installation

Clone and install the CLI:

```sh
$ git clone https://github.com/skyoo2003/kvs.git
$ cd kvs
$ go install ./cmd/kvs
```

## Library Usage

Import `github.com/skyoo2003/kvs` to use KVS as a library:

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

## License

MIT License. See [LICENSE](https://github.com/skyoo2003/kvs/blob/main/LICENSE) for details.
