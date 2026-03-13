// Package main implements to execute the program
package main

import (
	"flag"
	"fmt"

	"github.com/skyoo2003/kvs"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("v", false, "print version")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	store := kvs.NewStore()
	if err := store.Put("status", "ready"); err != nil {
		panic(err)
	}

	value, err := store.Get("status")
	if err != nil {
		panic(err)
	}

	fmt.Println(value)
}
