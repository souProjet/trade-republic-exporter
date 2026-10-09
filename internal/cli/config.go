package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/souProjet/trade-republic-exporter/internal/config"
	"github.com/souProjet/trade-republic-exporter/internal/tui"
)

func newConfigCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show and edit your settings",
		Long: `Without a subcommand, opens the interactive settings editor.

Settings are stored in an INI file in your user configuration directory,
except the PIN, which goes to the system keychain. Environment variables
override both: ` + config.EnvPhoneNumber + `, ` + config.EnvPIN + `, ` + config.EnvDeviceInfo + `.`,
		Example: `  tr-export config                          # interactive editor
  tr-export config show                     # every setting and where it comes from
  tr-export config set export.format json
  tr-export config set account.pin          # prompts, never echoed
  tr-export config unset export.datasets    # back to the default`,
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
			if err := tui.EditConfig(cmd.Context(), store); err != nil {
				if errors.Is(err, tui.ErrAborted) {
					notice(w, "Nothing changed")
					return nil
				}
				return err
			}
			success(w, "Saved to "+store.Path())
			return showConfig(w, store)
		},
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:     "show",
			Aliases: []string{"list", "ls"},
			Short:   "Show every setting, its value and where it comes from",
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
			Use:               "get KEY",
			Short:             "Print the effective value of a setting",
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
					return errors.New("the PIN is never printed; tr-export config show tells whether it is set")
				}
				fmt.Fprintln(cmd.OutOrStdout(), store.Resolve(k.Name).Value)
				return nil
			},
		},
		&cobra.Command{
			Use:   "set KEY [VALUE]",
			Short: "Change a setting",
			Long: `Change a setting. The PIN is not accepted as an argument, which would leave
it in your shell history: run "tr-export config set account.pin" and type it,
or pipe it in with "-" as the value.`,
			Args:              cobra.RangeArgs(1, 2),
			ValidArgsFunction: valueCompletion,
			RunE: func(cmd *cobra.Command, args []string) error {
				return setConfig(cmd, g, args)
			},
		},
		&cobra.Command{
			Use:               "unset KEY",
			Short:             "Remove a setting so its default applies",
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
				success(cmd.OutOrStdout(), args[0]+" reset to "+describeValue(store.Resolve(args[0])))
				return nil
			},
		},
		&cobra.Command{
			Use:   "path",
			Short: "Print the location of the configuration file",
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
			Short: "Open the configuration file in $VISUAL or $EDITOR",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return editConfigFile(cmd, g)
			},
		},
		newResetCmd(g),
	)
	return cmd
}

func showConfig(w interface{ Write([]byte) (int, error) }, store *config.Store) error {
	location := store.Path()
	if !store.Exists() {
		location += " (not created yet)"
	}
	heading(w, "Settings", location)

	rows := make([][]string, 0, len(config.Keys))
	for _, k := range config.Keys {
		v := store.Resolve(k.Name)
		rows = append(rows, []string{k.Name, describeValue(v), string(v.Source)})
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
		return "not set"
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
		success(w, "PIN saved to the "+config.KeychainName())
		return nil
	}

	if len(args) < 2 {
		hint := ""
		if len(k.Choices) > 0 {
			hint = " (one of: " + strings.Join(k.Choices, ", ") + ")"
		}
		return fmt.Errorf("missing value for %s%s", k.Name, hint)
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
			return "", fmt.Errorf("read PIN from stdin: %w", err)
		}
		return strings.TrimSpace(line), nil
	case len(args) == 2:
		return "", errors.New(`refusing a PIN on the command line, where it would stay in your shell history: run "tr-export config set account.pin" and type it, or pipe it with "-"`)
	case !term.IsTerminal(int(os.Stdin.Fd())):
		return "", errNotInteractive
	}

	fmt.Fprint(os.Stderr, "  "+lipgloss.NewStyle().Foreground(accent).Render("?")+" PIN (hidden): ")
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
		return fmt.Errorf("run %s: %w", editor, err)
	}

	reloaded, err := config.Open(store.Path())
	if err != nil {
		return err
	}
	if _, err := reloaded.Settings(); err != nil {
		return fmt.Errorf("the file has invalid settings: %w", err)
	}
	success(cmd.OutOrStdout(), "Settings are valid")
	return nil
}

func newResetCmd(g *globals) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Delete the configuration file and the stored PIN",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := g.openStore(cmd)
			if err != nil {
				return err
			}
			if !yes {
				if !interactive() {
					return errors.New("pass --yes to reset without a prompt")
				}
				confirm := false
				err := huh.NewConfirm().
					Title("Delete your settings?").
					Description(store.Path() + "\nand the PIN in the " + config.KeychainName() + ". Exported files are kept.").
					Affirmative("Delete").
					Negative("Keep").
					Value(&confirm).
					Run()
				if err != nil || !confirm {
					notice(cmd.OutOrStdout(), "Nothing deleted")
					return nil
				}
			}

			if err := config.DeletePIN(store.Resolve("account.phone_number").Value); err != nil {
				return err
			}
			if err := os.Remove(store.Path()); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			success(cmd.OutOrStdout(), "Settings and PIN deleted")
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}

func keyCompletion(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names := make([]string, len(config.Keys))
	for i, k := range config.Keys {
		names[i] = k.Name + "\t" + k.Description
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
