// Command tr-export exports Trade Republic accounts, holdings, balances and
// timelines to CSV or JSON files.
package main

import (
	"context"
	"os"
	"runtime/debug"

	"github.com/souProjet/trade-republic-exporter/internal/cli"
)

// version is set at build time by GoReleaser.
var version = ""

func main() {
	if err := cli.Execute(context.Background(), resolveVersion()); err != nil {
		os.Exit(1)
	}
}

// resolveVersion prefers the release version, then the module version the Go
// toolchain records for "go install", then "dev".
func resolveVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
