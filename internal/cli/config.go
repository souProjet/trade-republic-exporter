package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/souProjet/trade-republic-exporter/internal/config"
	"github.com/souProjet/trade-republic-exporter/internal/i18n"
	"github.com/souProjet/trade-republic-exporter/internal/tui"
)

func newConfigCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: i18n.T("Show and edit your settings"),
		Long: i18n.T(`Without a subcommand, opens the settings screen.

Settings are stored in an INI file in your user configuration directory,
except the PIN, which goes to the system keychain. Environment variables
override both: %s, %s, %s.`, config.EnvPhoneNumber, config.EnvPIN, config.EnvDeviceInfo),
		Example: i18n.T(`  tr-export config                          # settings screen
  tr-export config show                     # every setting and where it comes from
  tr-export config set export.format json
  tr-export config set interface.language fr
  tr-export config set account.pin          # prompts, never echoed
  tr-export config unset export.datasets    # back to the default`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := g.openStore(cmd)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if !interactive() {
				return showConfig(w, store)
			}
			if err := tui.EditSettings(cmd.Context(), store, false); err != nil {
				if errors.Is(err, tui.ErrAborted) {
					notice(w, i18n.T("Nothing changed"))
					return nil
				}
				return err
			}
			success(w, i18n.T("Saved to %s", store.Path()))
			return showConfig(w, store)
		},
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:     "show",
			Aliases: []string{"list", "ls"},
			Short:   i18n.T("Show every setting, its value and where it comes from"),
			Args:    cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				store, err := g.openStore(cmd)
				if err != nil {
					return err
				}
				return showConfig(cmd.OutOrStdout(), store)
			},
		},
		&cobra.Command{
			Use:               i18n.T("get KEY"),
			Short:             i18n.T("Print the effective value of a setting"),
			Args:              cobra.ExactArgs(1),
			ValidArgsFunction: keyCompletion,
			RunE: func(cmd *cobra.Command, args []string) error {
				store, err := g.openStore(cmd)
				if err != nil {
					return err
				}
				k, err := config.Lookup(args[0])
				if err != nil {
					return err
				}
				if k.Kind == config.KindSecret {
					return errors.New(i18n.T("the PIN is never printed; tr-export config show tells whether it is set"))
				}
				fmt.Fprintln(cmd.OutOrStdout(), store.Resolve(k.Name).Value)
				return nil
			},
		},
		&cobra.Command{
			Use:   i18n.T("set KEY [VALUE]"),
			Short: i18n.T("Change a setting"),
			Long: i18n.T(`Change a setting. The PIN is not accepted as an argument, which would leave
it in your shell history: run "tr-export config set account.pin" and type it,
or pipe it in with "-" as the value.`),
			Args:              cobra.RangeArgs(1, 2),
			ValidArgsFunction: valueCompletion,
			RunE: func(cmd *cobra.Command, args []string) error {
				return setConfig(cmd, g, args)
			},
		},
		&cobra.Command{
			Use:               i18n.T("unset KEY"),
			Short:             i18n.T("Remove a setting so its default applies"),
			Args:              cobra.ExactArgs(1),
			ValidArgsFunction: keyCompletion,
			RunE: func(cmd *cobra.Command, args []string) error {
				store, err := g.openStore(cmd)
				if err != nil {
					return err
				}
				if err := store.Unset(args[0]); err != nil {
					return err
				}
				if err := store.Save(); err != nil {
					return err
				}
				success(cmd.OutOrStdout(), i18n.T("%s reset to %s", args[0], describeValue(store.Resolve(args[0]))))
				return nil
			},
		},
		&cobra.Command{
			Use:   "path",
			Short: i18n.T("Print the location of the settings file"),
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				store, err := g.openStore(cmd)
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), store.Path())
				return nil
			},
		},
		&cobra.Command{
			Use:   "edit",
			Short: i18n.T("Open the settings file in $VISUAL or $EDITOR"),
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return editConfigFile(cmd, g)
			},
		},
		newResetCmd(g),
	)
	return cmd
}

func showConfig(w io.Writer, store *config.Store) error {
	location := shortHome(store.Path())
	if !store.Exists() {
		location += " " + i18n.T("(not created yet)")
	}
	heading(w, i18n.T("Settings"), location)

	rows := make([][]string, 0, len(config.Keys))
	for _, k := range config.Keys {
		v := store.Resolve(k.Name)
		source := ""
		if v.Source != config.SourceUnset {
			source = i18n.T(string(v.Source))
		}
		rows = append(rows, []string{k.Name, describeValue(v), source})
	}
	table(w, rows)

	if _, err := store.Settings(); err != nil {
		fmt.Fprintln(w)
		warn(w, err.Error())
	}
	fmt.Fprintln(w)
	return nil
}

// describeValue renders a value for display: secrets are masked and long
// opaque values shortened.
func describeValue(v config.Value) string {
	switch {
	case v.Source == config.SourceUnset:
		return i18n.T("not set")
	case v.Key.Kind == config.KindSecret:
		return "••••"
	case len(v.Value) > 32:
		return v.Value[:29] + "..."
	default:
		return v.Value
	}
}

func setConfig(cmd *cobra.Command, g *globals, args []string) error {
	store, err := g.openStore(cmd)
	if err != nil {
		return err
	}
	k, err := config.Lookup(args[0])
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()

	if k.Kind == config.KindSecret {
		pin, err := readSecret(args)
		if err != nil {
			return err
		}
		if err := store.Set(k.Name, pin); err != nil {
			return err
		}
		success(w, i18n.T("PIN saved to the %s", config.KeychainName()))
		return nil
	}

	if len(args) < 2 {
		if len(k.Choices) > 0 {
			return errors.New(i18n.T("missing value for %s (one of: %s)", k.Name, strings.Join(k.Choices, ", ")))
		}
		return errors.New(i18n.T("missing value for %s", k.Name))
	}
	if err := store.Set(k.Name, args[1]); err != nil {
		return err
	}
	if err := store.Save(); err != nil {
		return err
	}
	success(w, k.Name+" = "+describeValue(store.Resolve(k.Name)))
	return nil
}

// readSecret reads the PIN from a hidden prompt, or from stdin when the value
// is "-".
func readSecret(args []string) (string, error) {
	switch {
	case len(args) == 2 && args[1] == "-":
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("%s: %w", i18n.T("could not read the PIN from stdin"), err)
		}
		return strings.TrimSpace(line), nil
	case len(args) == 2:
		return "", errors.New(i18n.T(`refusing a PIN on the command line, where it would stay in your shell history: run "tr-export config set account.pin" and type it, or pipe it with "-"`))
	case !term.IsTerminal(int(os.Stdin.Fd())):
		return "", errors.New(i18n.T("this needs an interactive terminal"))
	}

	fmt.Fprint(os.Stderr, "  "+lipgloss.NewStyle().Foreground(accent).Render("?")+" "+i18n.T("PIN (hidden):")+" ")
	pin, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(pin)), nil
}

func editConfigFile(cmd *cobra.Command, g *globals) error {
	store, err := g.openStore(cmd)
	if err != nil {
		return err
	}
	if !store.Exists() {
		if err := store.Save(); err != nil {
			return err
		}
	}

	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
		if runtime.GOOS == "windows" {
			editor = "notepad"
		}
	}
	// $EDITOR may carry arguments, such as "code --wait".
	parts := strings.Fields(editor)
	run := exec.CommandContext(cmd.Context(), parts[0], append(parts[1:], store.Path())...)
	run.Stdin, run.Stdout, run.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := run.Run(); err != nil {
		return fmt.Errorf("%s: %w", i18n.T("could not run %s", editor), err)
	}

	reloaded, err := config.Open(store.Path())
	if err != nil {
		return err
	}
	if _, err := reloaded.Settings(); err != nil {
		return fmt.Errorf("%s\n%w", i18n.T("The file has invalid settings:"), err)
	}
	success(cmd.OutOrStdout(), i18n.T("Settings are valid"))
	return nil
}

func newResetCmd(g *globals) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "reset",
		Short: i18n.T("Delete the settings file and the stored PIN"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := g.openStore(cmd)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if !yes {
				if !interactive() {
					return errors.New(i18n.T("pass --yes to reset without a prompt"))
				}
				question := i18n.T("Delete %s and the PIN stored in the %s? Exported files are kept. [y/N]", shortHome(store.Path()), config.KeychainName())
				fmt.Fprint(w, "  "+lipgloss.NewStyle().Foreground(accent).Render("?")+" "+question+" ")
				answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
				switch strings.ToLower(strings.TrimSpace(answer)) {
				case "y", "yes", "o", "oui":
				default:
					notice(w, i18n.T("Nothing deleted"))
					return nil
				}
			}

			if err := config.DeletePIN(store.Resolve("account.phone_number").Value); err != nil {
				return err
			}
			if err := os.Remove(store.Path()); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			success(w, i18n.T("Settings and PIN deleted"))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, i18n.T("do not ask for confirmation"))
	return cmd
}

func shortHome(path string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

func keyCompletion(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names := make([]string, len(config.Keys))
	for i, k := range config.Keys {
		names[i] = k.Name + "\t" + i18n.T(k.Description)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

func valueCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return keyCompletion(cmd, args, toComplete)
	}
	k, err := config.Lookup(args[0])
	if err != nil || len(args) > 1 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	switch k.Kind {
	case config.KindBool:
		return []string{"true", "false"}, cobra.ShellCompDirectiveNoFileComp
	case config.KindChoice, config.KindList:
		return k.Choices, cobra.ShellCompDirectiveNoFileComp
	}
	if k.Name == "export.output_dir" {
		return nil, cobra.ShellCompDirectiveFilterDirs
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}
