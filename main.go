// Command trade-republic-exporter exports Trade Republic accounts, holdings,
// balances and timelines to CSV or JSON files.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/souProjet/trade-republic-exporter/internal/app"
	"github.com/souProjet/trade-republic-exporter/internal/config"
	"github.com/souProjet/trade-republic-exporter/internal/export"
	"github.com/souProjet/trade-republic-exporter/internal/ui"
)

const appName = "trade-republic-exporter"

func main() {
	var (
		configPath  = flag.String("config", "config.ini", "path to the INI configuration file")
		format      = flag.String("format", string(export.CSV), "output format: csv or json")
		outputDir   = flag.String("out", "out", "directory to write the exported files to")
		details     = flag.Bool("details", false, "fetch the detail view of every transaction (one extra request per transaction)")
		quiet       = flag.Bool("quiet", false, "only report warnings and errors")
		showVersion = flag.Bool("version", false, "print the version and exit")
	)
	flag.Usage = usage
	flag.Parse()

	if *showVersion {
		fmt.Println(appName, version())
		return
	}

	out := ui.New(os.Stderr, os.Stdin, *quiet)
	out.Title(appName, version())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fail(out, err)
	}

	// Flags win over the file, but only when explicitly passed.
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "format":
			cfg.Format = export.Format(*format)
		case "out":
			cfg.OutputDir = *outputDir
		case "details":
			cfg.ExtractDetails = *details
		}
	})

	if err := cfg.Validate(); err != nil {
		fail(out, err)
	}
	if err := app.Run(ctx, cfg, out); err != nil {
		fail(out, err)
	}
}

func fail(out *ui.UI, err error) {
	out.Error(err)
	os.Exit(1)
}

func usage() {
	fmt.Fprintf(os.Stderr, `%s exports your Trade Republic data to CSV or JSON.

Usage:
  %s [flags]

Flags:
`, appName, appName)
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, `
Examples:
  %s                                 # read config.ini, write CSV into out/
  %s -format json -out exports       # JSON files into exports/
  %s -details                        # enrich every transaction (slower)

Credentials can also come from the environment: %s, %s.
`, appName, appName, appName, config.EnvPhoneNumber, config.EnvPIN)
}

// version reports the module version chosen by the Go toolchain, which is set
// for binaries installed with "go install".
func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return "dev"
	}
	return info.Main.Version
}
