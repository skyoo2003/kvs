// Package main implements to execute the program
package main

import (
	"fmt"

	"github.com/skyoo2003/kvs/internal/kvs"
)

func main() {
	store := kvs.Store{}
	fmt.Println(store)
}
