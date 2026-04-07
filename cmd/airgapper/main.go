// Package main is the entrypoint for the Universal Airgapper CLI. Build-time
// variables (version, commit, date) are injected via ldflags.
package main

import (
	"fmt"
	"os"

	"github.com/fullstacks-gmbh/airgapper/internal/cli"
)

// Set via ldflags at build time.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cmd := cli.NewRootCmd(version, commit, date)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
