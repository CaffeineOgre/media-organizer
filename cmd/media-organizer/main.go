// Package main provides the entry point for the media-organizer tool.
package main

import (
	"fmt"
	"os"

	"github.com/CaffeineOgre/media-organizer/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
