// Package cli defines the command-line interface: the export as the root
// command, and the config command family to read and edit settings.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/souProjet/trade-republic-exporter/internal/config"
)

// legacyConfig is where v0.1 kept its settings.
const legacyConfig = "config.ini"

// Execute runs the command line.
func Execute(ctx context.Context, version string) error {
	root := newRootCmd(version)
	return fang.Execute(ctx, root,
		fang.WithVersion(version),
		fang.WithoutManpage(),
		fang.WithNotifySignal(os.Interrupt, syscall.SIGTERM),
	)
}

// globals are the flags shared by every command.
type globals struct {
	configPath string
}

// openStore opens the configuration, importing a v0.1 config.ini from the
// working directory the first time.
func (g *globals) openStore(cmd *cobra.Command) (*config.Store, error) {
	path := g.configPath
	explicit := path != ""
	if !explicit {
		var err error
		if path, err = config.DefaultPath(); err != nil {
			return nil, fmt.Errorf("locate the configuration directory: %w (pass --config)", err)
		}
	}
	store, err := config.Open(path)
	if err != nil {
		return nil, err
	}
	if explicit || store.Exists() {
		return store, nil
	}

	m, err := store.MigrateLegacy(legacyConfig)
	if err != nil || m == nil {
		return store, err
	}
	w := cmd.ErrOrStderr()
	notice(w, fmt.Sprintf("Imported %s into %s", m.From, store.Path()))
	switch {
	case m.PINStored:
		notice(w, "Your PIN moved to the "+config.KeychainName()+". "+m.From+" still holds it in clear text: delete that file.")
	case m.PINError != nil:
		warn(w, fmt.Sprintf("PIN not imported: %v", m.PINError))
	}
	return store, nil
}

func newRootCmd(version string) *cobra.Command {
	g := &globals{}
	opts := &exportFlags{}

	root := &cobra.Command{
		Use:   "tr-export",
		Short: "Export your Trade Republic accounts, holdings and transactions",
		Long: `Export every Trade Republic account, securities account and PEA alike, with
holdings (crypto included), cash, transactions, activity log, savings plans and
open orders, to CSV or JSON.

Run it once to set up your phone number and PIN; settings live in your user
configuration directory and the PIN in the system keychain.`,
		Example: `  # Export with your saved settings
  tr-export

  # JSON into another folder, just holdings and transactions
  tr-export --format json --out ~/exports --datasets positions,transactions

  # Preview the interface without signing in
  tr-export --demo

  # Change settings
  tr-export config
  tr-export config set export.format json`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runExport(cmd, g, opts, version)
		},
	}

	root.PersistentFlags().StringVar(&g.configPath, "config", "", "configuration file (default: "+defaultPathHint()+")")
	opts.register(root)
	root.AddCommand(newConfigCmd(g))
	return root
}

func defaultPathHint() string {
	path, err := config.DefaultPath()
	if err != nil {
		return "the user configuration directory"
	}
	if home, err := os.UserHomeDir(); err == nil && len(path) > len(home) && path[:len(home)] == home {
		return "~" + path[len(home):]
	}
	return path
}

// interactive reports whether a person is at the keyboard and screen.
func interactive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

var errNotInteractive = errors.New("this needs an interactive terminal")
