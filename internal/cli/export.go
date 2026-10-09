package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/souProjet/trade-republic-exporter/internal/app"
	"github.com/souProjet/trade-republic-exporter/internal/config"
	"github.com/souProjet/trade-republic-exporter/internal/export"
	"github.com/souProjet/trade-republic-exporter/internal/report"
	"github.com/souProjet/trade-republic-exporter/internal/tui"
	"github.com/souProjet/trade-republic-exporter/internal/ui"
)

// exportFlags override the saved settings for one run.
type exportFlags struct {
	format    string
	dialect   string
	outputDir string
	datasets  []string
	details   bool
	plain     bool
	quiet     bool
	demo      bool
}

func (f *exportFlags) register(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVarP(&f.format, "format", "f", "", "output format: csv or json")
	flags.StringVar(&f.dialect, "dialect", "", "dialect for CSV files: european or standard")
	flags.StringVarP(&f.outputDir, "out", "o", "", "output directory")
	flags.StringSliceVar(&f.datasets, "datasets", nil, "datasets to export, comma separated: "+strings.Join(datasetNames(), ", "))
	flags.BoolVarP(&f.details, "details", "d", false, "fetch the detail view of every transaction (slower)")
	flags.BoolVar(&f.plain, "plain", false, "plain line output instead of the full-screen interface")
	flags.BoolVarP(&f.quiet, "quiet", "q", false, "only print warnings and errors (implies --plain)")
	flags.BoolVar(&f.demo, "demo", false, "preview the interface with sample data, without signing in or writing files")

	_ = cmd.RegisterFlagCompletionFunc("format", fixedCompletion("csv", "json"))
	_ = cmd.RegisterFlagCompletionFunc("dialect", fixedCompletion(export.European.Name, export.Standard.Name))
	_ = cmd.RegisterFlagCompletionFunc("datasets", fixedCompletion(datasetNames()...))
}

// apply overrides settings with the flags that were passed explicitly.
func (f *exportFlags) apply(cmd *cobra.Command, s *config.Settings) error {
	flags := cmd.Flags()
	if flags.Changed("format") {
		format, err := export.ParseFormat(f.format)
		if err != nil {
			return err
		}
		s.Format = format
	}
	if flags.Changed("dialect") {
		dialect, err := export.ParseDialect(f.dialect)
		if err != nil {
			return err
		}
		s.Dialect = dialect
	}
	if flags.Changed("out") {
		s.OutputDir = f.outputDir
	}
	if flags.Changed("details") {
		s.Details = f.details
	}
	if flags.Changed("datasets") {
		for _, name := range f.datasets {
			if _, ok := export.LookupDataset(name); !ok {
				return fmt.Errorf("unknown dataset %q (valid: %s)", name, strings.Join(datasetNames(), ", "))
			}
		}
		s.Datasets = f.datasets
	}
	return nil
}

func runExport(cmd *cobra.Command, g *globals, flags *exportFlags, version string) error {
	ctx := cmd.Context()
	store, err := g.openStore(cmd)
	if err != nil {
		return err
	}
	settings, err := store.Settings()
	if err != nil {
		return fmt.Errorf("invalid settings in %s: %w\nFix them with: tr-export config", store.Path(), err)
	}
	if err := flags.apply(cmd, &settings); err != nil {
		return err
	}

	// First run: walk through the settings before signing in.
	if settings.PhoneNumber == "" && !flags.demo {
		if !interactive() {
			return fmt.Errorf("no phone number configured: run tr-export config, or set %s and %s", config.EnvPhoneNumber, config.EnvPIN)
		}
		notice(cmd.ErrOrStderr(), "First run: let's set up your account. Your settings will be saved to "+store.Path())
		if err := tui.EditConfig(ctx, store); err != nil {
			return err
		}
		if settings, err = store.Settings(); err != nil {
			return err
		}
		if err := flags.apply(cmd, &settings); err != nil {
			return err
		}
	}

	opts := app.Options{
		PhoneNumber: settings.PhoneNumber,
		PIN:         settings.PIN,
		DeviceInfo:  settings.DeviceInfo,
		WAFToken:    settings.WAFToken,
		Format:      settings.Format,
		Dialect:     settings.Dialect,
		OutputDir:   settings.OutputDir,
		Details:     settings.Details,
		Datasets:    settings.Datasets,
		SaveDeviceInfo: func(v string) error {
			if err := store.Set("account.device_info", v); err != nil {
				return err
			}
			return store.Save()
		},
	}
	job := app.Run
	if flags.demo {
		opts.SaveDeviceInfo = nil
		job = app.Demo
	}

	if useFullscreen(flags, settings.Interface) {
		res, err := tui.Run(ctx, version, describe(settings, flags.demo), func(ctx context.Context, r report.Reporter) error {
			return job(ctx, opts, r)
		})
		if err != nil {
			return err
		}
		printResult(cmd.OutOrStdout(), res)
		if res.Interrupted {
			return errors.New("interrupted before the export finished")
		}
		return res.Err
	}

	out := ui.New(cmd.ErrOrStderr(), os.Stdin, flags.quiet)
	out.Title("trade-republic-exporter", version)
	return job(ctx, opts, out.Reporter())
}

func useFullscreen(flags *exportFlags, mode string) bool {
	if flags.plain || flags.quiet || !interactive() {
		return false
	}
	return mode != config.ModePlain
}

// describe lists the settings of the run for the dashboard.
func describe(s config.Settings, demo bool) [][2]string {
	format := strings.ToUpper(string(s.Format))
	if s.Format == export.CSV {
		format += " · " + s.Dialect.Name
	}
	datasets := "all"
	if len(s.Datasets) > 0 {
		datasets = strings.Join(s.Datasets, ", ")
	}
	details := "off"
	if s.Details {
		details = "on"
	}
	info := [][2]string{
		{"Format", format},
		{"Output", s.OutputDir},
		{"Datasets", datasets},
		{"Details", details},
	}
	if demo {
		info = append(info, [2]string{"Mode", "demo, nothing is written"})
	}
	return info
}

// printResult leaves a record on the regular screen once the full-screen
// interface has closed.
func printResult(w io.Writer, res tui.Result) {
	s := res.Summary
	if s == nil {
		return
	}
	headline := fmt.Sprintf("%d files, %d rows in %s", len(s.Files), s.Rows(), s.Elapsed.Round(100_000_000))
	if s.Failed > 0 {
		headline += fmt.Sprintf(", %d failed", s.Failed)
	}
	success(w, headline)
	rows := make([][]string, len(s.Files))
	for i, f := range s.Files {
		rows[i] = []string{f.Path, fmt.Sprintf("%d rows", f.Rows)}
	}
	table(w, rows)
	for _, msg := range res.Warnings {
		warn(w, msg)
	}
	fmt.Fprintln(w)
}

func datasetNames() []string {
	names := make([]string, len(export.Datasets))
	for i, d := range export.Datasets {
		names[i] = d.Name
	}
	return names
}

func fixedCompletion(values ...string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return values, cobra.ShellCompDirectiveNoFileComp
	}
}
