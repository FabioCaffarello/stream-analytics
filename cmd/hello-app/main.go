// Package main defines the hello workspace executable.
package main

import (
	"fmt"

	"github.com/market-raccoon/hello-lib/hello"
)

func main() {
	fmt.Println(hello.Message("workspace"))
}
