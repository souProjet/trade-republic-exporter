// Package app runs an export: sign in, discover the accounts, then fetch and
// write every selected dataset, reporting progress through report.Reporter.
package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/souProjet/trade-republic-exporter/internal/auth"
	"github.com/souProjet/trade-republic-exporter/internal/device"
	"github.com/souProjet/trade-republic-exporter/internal/export"
	"github.com/souProjet/trade-republic-exporter/internal/i18n"
	"github.com/souProjet/trade-republic-exporter/internal/report"
	"github.com/souProjet/trade-republic-exporter/internal/trws"
	"github.com/souProjet/trade-republic-exporter/internal/waf"
)

// Phases of a run, in order.
const (
	PhaseAuthentication = "Authentication"
	PhaseSession        = "Session"
	PhaseExport         = "Export"
)

// Options is everything a run needs.
type Options struct {
	PhoneNumber string
	// PIN is asked for interactively when empty.
	PIN        string
	DeviceInfo string
	WAFToken   string

	Format    export.Format
	Dialect   export.Dialect
	OutputDir string
	Details   bool
	// Datasets restricts the export to these names; empty selects all.
	Datasets []string

	// SaveDeviceInfo persists a newly generated device identity. Optional.
	SaveDeviceInfo func(string) error
}

// Run executes a full export. It returns an error when the session cannot be
// established, or when at least one dataset failed after the others were
// written.
func Run(ctx context.Context, opts Options, r report.Reporter) error {
	started := time.Now()

	session, err := signIn(ctx, opts, r)
	if err != nil {
		return err
	}

	r.Phase(i18n.T(PhaseSession))
	dialTask := r.Task(i18n.T("WebSocket"))
	client, err := trws.Dial(ctx, session)
	if err != nil {
		dialTask.Fail(err)
		return err
	}
	defer client.Close()
	dialTask.Done(i18n.T("connected"))

	accounts := discoverAccounts(ctx, client, r)

	r.Phase(i18n.T(PhaseExport))
	files, failures := collect(ctx, client, opts, accounts, r)
	r.Finish(report.Summary{
		Files:     files,
		Failed:    failures,
		Elapsed:   time.Since(started),
		OutputDir: opts.OutputDir,
	})

	if err := ctx.Err(); err != nil {
		return err
	}
	if failures > 0 {
		return errors.New(i18n.T("%d dataset(s) could not be exported", failures))
	}
	return nil
}

// signIn resolves the device identity and WAF token, then completes the
// two-factor login and returns the session token.
func signIn(ctx context.Context, opts Options, r report.Reporter) (string, error) {
	r.Phase(i18n.T(PhaseAuthentication))

	deviceTask := r.Task(i18n.T("Device identity"))
	deviceInfo := opts.DeviceInfo
	switch {
	case deviceInfo != "":
		deviceTask.Done(i18n.T("reused"))
	default:
		generated, err := device.Info()
		if err != nil {
			deviceTask.Fail(err)
			return "", err
		}
		deviceInfo = generated
		if opts.SaveDeviceInfo == nil {
			deviceTask.Done(i18n.T("generated"))
		} else if err := opts.SaveDeviceInfo(generated); err != nil {
			deviceTask.Done(i18n.T("generated, not saved"))
			r.Warn(i18n.T("Device identity not saved: %v", err))
		} else {
			deviceTask.Done(i18n.T("generated and saved"))
		}
	}

	wafTask := r.Task(i18n.T("AWS WAF token"))
	wafToken := opts.WAFToken
	if wafToken == "" {
		wafTask.Update(i18n.T("solving the challenge in headless Chrome"))
		token, err := waf.Token(ctx)
		if err != nil {
			wafTask.Fail(err)
			return "", err
		}
		wafToken = token
		wafTask.Done(i18n.T("solved"))
	} else {
		wafTask.Done(i18n.T("from environment"))
	}

	pin := opts.PIN
	if pin == "" {
		answer, err := r.Ask(report.Prompt{
			Title:       i18n.T("PIN"),
			Description: i18n.T("Your 4-digit Trade Republic PIN. Run `tr-export config set account.pin` to store it in the keychain and skip this step."),
			Placeholder: i18n.T("4 digits"),
			Secret:      true,
		})
		if err != nil {
			return "", err
		}
		pin = answer
	}

	client := auth.New(wafToken, deviceInfo)
	loginTask := r.Task(i18n.T("Login"))
	process, err := client.Start(ctx, opts.PhoneNumber, pin)
	if err != nil {
		loginTask.Fail(err)
		return "", err
	}
	loginTask.Done(RedactPhone(opts.PhoneNumber))

	code, err := askCode(ctx, r, process, opts.PhoneNumber)
	if err != nil {
		return "", err
	}

	verifyTask := r.Task(i18n.T("Two-factor code"))
	session, err := process.Verify(ctx, code)
	if err != nil {
		verifyTask.Fail(err)
		return "", err
	}
	verifyTask.Done(i18n.T("session established"))
	return session, nil
}

// askCode reads the two-factor code, offering to receive it by SMS instead.
func askCode(ctx context.Context, r report.Reporter, process *auth.Process, phone string) (string, error) {
	prompt := report.Prompt{
		Title:            i18n.T("Two-factor code"),
		Description:      i18n.T("Enter the code shown in the Trade Republic app."),
		Placeholder:      i18n.T("code"),
		Alternative:      "sms",
		AlternativeLabel: i18n.T("send by SMS"),
	}
	if process.CountdownSeconds > 0 {
		prompt.Deadline = time.Now().Add(time.Duration(process.CountdownSeconds) * time.Second)
	}

	code, err := r.Ask(prompt)
	if err != nil || !strings.EqualFold(code, prompt.Alternative) {
		return code, err
	}

	smsTask := r.Task(i18n.T("SMS fallback"))
	if err := process.Resend(ctx); err != nil {
		smsTask.Fail(err)
		return "", err
	}
	smsTask.Done(i18n.T("code sent"))
	return r.Ask(report.Prompt{
		Title:       i18n.T("SMS code"),
		Description: i18n.T("Enter the code sent to %s.", RedactPhone(phone)),
		Placeholder: i18n.T("code"),
	})
}

// discoverAccounts lists the customer's account pairs. A failure is not fatal:
// the account-wide datasets can still be exported.
func discoverAccounts(ctx context.Context, client *trws.Client, r report.Reporter) []trws.Account {
	task := r.Task(i18n.T("Accounts"))
	accounts, _, err := client.Accounts(ctx)
	switch {
	case err != nil:
		task.Skip(i18n.T("unavailable (%s), using the default account", compact(err)))
		return nil
	case len(accounts) == 0:
		task.Skip(i18n.T("none reported, using the default account"))
		return nil
	}
	task.Done(i18n.T("%d found", len(accounts)))

	views := make([]report.Account, len(accounts))
	for i, a := range accounts {
		views[i] = report.Account{
			Label:      i18n.T(a.Label()),
			Securities: a.SecuritiesAccountNumber,
			Cash:       a.CashAccountNumber,
			Currency:   a.Currency,
		}
	}
	r.Accounts(views)
	return accounts
}

// RedactPhone keeps the country code and the last two digits.
func RedactPhone(phone string) string {
	if len(phone) < 6 {
		return "•••"
	}
	return phone[:3] + strings.Repeat("•", len(phone)-5) + phone[len(phone)-2:]
}

// compact shortens an error to one readable line.
func compact(err error) string {
	var apiErr *trws.APIError
	if errors.As(err, &apiErr) {
		return i18n.T("rejected by Trade Republic")
	}
	msg := strings.ReplaceAll(err.Error(), "\n", " ")
	if len(msg) > 90 {
		msg = msg[:90] + "..."
	}
	return msg
}
