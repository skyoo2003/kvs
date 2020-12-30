// Package main implements to execute the program
package main

import (
	"fmt"

	"github.com/skyoo2003/kvs/pkg/kv"
)

func main() {
	store := kv.Store{}
	fmt.Println(store)
}
