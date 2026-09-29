package main

import (
	"fmt"
	"os"

	app "github.com/skyoo2003/kvs/internal/app/kvs"
)

var version = "dev"

func main() {
	if err := app.Execute(os.Args[1:], os.Stdout, os.Stderr, version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
