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
	"github.com/souProjet/trade-republic-exporter/internal/i18n"
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
	flags.StringVarP(&f.format, "format", "f", "", i18n.T("output format: csv or json"))
	flags.StringVar(&f.dialect, "dialect", "", i18n.T("dialect for CSV files: european or standard"))
	flags.StringVarP(&f.outputDir, "out", "o", "", i18n.T("output directory"))
	flags.StringSliceVar(&f.datasets, "datasets", nil, i18n.T("datasets to export, comma separated: %s", strings.Join(datasetNames(), ", ")))
	flags.BoolVarP(&f.details, "details", "d", false, i18n.T("fetch the detail view of every transaction (slower)"))
	flags.BoolVar(&f.plain, "plain", false, i18n.T("plain line output instead of the full-screen interface"))
	flags.BoolVarP(&f.quiet, "quiet", "q", false, i18n.T("only print warnings and errors (implies --plain)"))
	flags.BoolVar(&f.demo, "demo", false, i18n.T("preview the interface with sample data, without signing in or writing files"))

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
				return errors.New(i18n.T("unknown dataset %q (valid: %s)", name, strings.Join(datasetNames(), ", ")))
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
		return fmt.Errorf("%s\n%w", i18n.T("Invalid settings in %s, fix them with: tr-export config", store.Path()), err)
	}
	if err := flags.apply(cmd, &settings); err != nil {
		return err
	}

	// First run: walk through the settings before signing in.
	if settings.PhoneNumber == "" && !flags.demo {
		if !interactive() {
			return errors.New(i18n.T("no phone number configured: run tr-export config, or set %s and %s", config.EnvPhoneNumber, config.EnvPIN))
		}
		if err := tui.EditSettings(ctx, store, true); err != nil {
			if errors.Is(err, tui.ErrAborted) {
				notice(cmd.ErrOrStderr(), i18n.T("Nothing saved, export canceled."))
				return nil
			}
			return err
		}
		i18n.Set(i18n.Language(store.Resolve("interface.language").Value))
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
			return errors.New(i18n.T("interrupted before the export finished"))
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
		dialect := i18n.T("European")
		if s.Dialect.Name == export.Standard.Name {
			dialect = i18n.T("Standard")
		}
		format += " · " + dialect
	}
	datasets := i18n.T("all")
	if len(s.Datasets) > 0 {
		datasets = strings.Join(s.Datasets, ", ")
	}
	details := i18n.T("off")
	if s.Details {
		details = i18n.T("on")
	}
	info := [][2]string{
		{i18n.T("Format"), format},
		{i18n.T("Output"), s.OutputDir},
		{i18n.T("Datasets"), datasets},
		{i18n.T("Details"), details},
	}
	if demo {
		info = append(info, [2]string{i18n.T("Mode"), i18n.T("demo, nothing is written")})
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
	headline := i18n.T("%d files, %d rows in %s", len(s.Files), s.Rows(), s.Elapsed.Round(100_000_000))
	if s.Failed > 0 {
		headline += i18n.T(", %d failed", s.Failed)
	}
	success(w, headline)
	rows := make([][]string, len(s.Files))
	for i, f := range s.Files {
		rows[i] = []string{f.Path, i18n.T("%d rows", f.Rows)}
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
