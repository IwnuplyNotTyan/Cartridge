package main

import (
	"fmt"
	"os"

	"cartridge/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "cartridge: %v\n", err)
		os.Exit(1)
	}
}
