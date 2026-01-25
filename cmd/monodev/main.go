package main

import (
	"fmt"
	"os"

	"github.com/danieljhkim/monodev/internal/cli"
)

var version = "dev"

func main() {
	cli.SetVersion(version)

	if err := cli.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
