// Package app wires the login, the WebSocket session and the export pipeline
// together, reporting progress through the ui package.
package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/souProjet/trade-republic-exporter/internal/auth"
	"github.com/souProjet/trade-republic-exporter/internal/config"
	"github.com/souProjet/trade-republic-exporter/internal/device"
	"github.com/souProjet/trade-republic-exporter/internal/trws"
	"github.com/souProjet/trade-republic-exporter/internal/ui"
	"github.com/souProjet/trade-republic-exporter/internal/waf"
)

// Run executes a full export. It returns an error when the session cannot be
// established, or when at least one dataset failed after the others were
// written.
func Run(ctx context.Context, cfg *config.Config, out *ui.UI) error {
	started := time.Now()

	session, err := signIn(ctx, cfg, out)
	if err != nil {
		return err
	}

	out.Section("Session")
	dialTask := out.Task("WebSocket")
	client, err := trws.Dial(ctx, session)
	if err != nil {
		dialTask.Fail(err)
		return err
	}
	defer client.Close()
	dialTask.Done("connected to api.traderepublic.com")

	accounts := discoverAccounts(ctx, client, out)

	results, failures := collect(ctx, client, cfg, accounts, out)
	report(out, results, failures, started)

	if failures > 0 {
		return fmt.Errorf("%d dataset(s) could not be exported", failures)
	}
	return nil
}

// signIn resolves the device identity and WAF token, then completes the
// two-factor login and returns the session token.
func signIn(ctx context.Context, cfg *config.Config, out *ui.UI) (string, error) {
	out.Section("Authentication")

	deviceTask := out.Task("Device identity")
	deviceInfo := cfg.DeviceInfo
	if deviceInfo == "" {
		generated, err := device.Info()
		if err != nil {
			deviceTask.Fail(err)
			return "", err
		}
		deviceInfo = generated
		deviceTask.Done("generated, set device_info to reuse it")
	} else {
		deviceTask.Done("from configuration")
	}

	wafTask := out.Task("AWS WAF token")
	wafToken := cfg.WAFToken
	if wafToken == "" {
		wafTask.Update("solving the challenge in headless Chrome")
		token, err := waf.Token(ctx)
		if err != nil {
			wafTask.Fail(err)
			return "", err
		}
		wafToken = token
		wafTask.Done("solved in headless Chrome")
	} else {
		wafTask.Done("from configuration")
	}

	client := auth.New(wafToken, deviceInfo)

	loginTask := out.Task("Login")
	process, err := client.Start(ctx, cfg.PhoneNumber, cfg.PIN)
	if err != nil {
		loginTask.Fail(err)
		return "", err
	}
	loginTask.Done("%s accepted", redactPhone(cfg.PhoneNumber))

	code, err := askCode(ctx, out, process)
	if err != nil {
		return "", err
	}

	verifyTask := out.Task("Two-factor code")
	session, err := process.Verify(ctx, code)
	if err != nil {
		verifyTask.Fail(err)
		return "", err
	}
	verifyTask.Done("session established")
	return session, nil
}

// askCode reads the two-factor code, offering an SMS fallback when the user
// answers "sms" instead of a code.
func askCode(ctx context.Context, out *ui.UI, process *auth.Process) (string, error) {
	label := "Two-factor code (or type sms to receive one by SMS):"
	if process.CountdownSeconds > 0 {
		label = fmt.Sprintf("Two-factor code, %ds left (or type sms):", process.CountdownSeconds)
	}

	code, err := out.Ask(label)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(code, "sms") {
		return code, nil
	}

	smsTask := out.Task("SMS fallback")
	if err := process.Resend(ctx); err != nil {
		smsTask.Fail(err)
		return "", err
	}
	smsTask.Done("code sent")
	return out.Ask("Code received by SMS:")
}

// discoverAccounts lists the customer's account pairs. A failure is not fatal:
// the account-wide datasets can still be exported.
func discoverAccounts(ctx context.Context, client *trws.Client, out *ui.UI) []trws.Account {
	task := out.Task("Accounts")
	accounts, _, err := client.Accounts(ctx)
	if err != nil {
		task.Skip("not available (%s), falling back to the default account", compact(err))
		return nil
	}
	if len(accounts) == 0 {
		task.Skip("none reported, falling back to the default account")
		return nil
	}
	task.Done("%d found", len(accounts))

	rows := make([][]string, 0, len(accounts))
	for _, account := range accounts {
		rows = append(rows, []string{account.Label(), account.SecuritiesAccountNumber, account.CashAccountNumber, account.Currency})
	}
	out.Table(rows)
	return accounts
}

func report(out *ui.UI, results []result, failures int, started time.Time) {
	out.Section("Files")
	rows := make([][]string, 0, len(results))
	total := 0
	for _, r := range results {
		total += r.rows
		rows = append(rows, []string{r.name, fmt.Sprintf("%d rows", r.rows), r.path})
	}
	out.Table(rows)

	summary := fmt.Sprintf("%d files, %d rows, %.1fs", len(results), total, time.Since(started).Seconds())
	if failures > 0 {
		summary += fmt.Sprintf(", %d failed", failures)
	}
	out.Section("Done")
	out.Detail("%s", summary)
}

// redactPhone keeps the country code and the last two digits.
func redactPhone(phone string) string {
	if len(phone) < 6 {
		return "***"
	}
	return phone[:3] + strings.Repeat("*", len(phone)-5) + phone[len(phone)-2:]
}

// compact shortens an error to one readable line.
func compact(err error) string {
	var apiErr *trws.APIError
	if errors.As(err, &apiErr) {
		return "rejected by Trade Republic"
	}
	msg := strings.ReplaceAll(err.Error(), "\n", " ")
	if len(msg) > 90 {
		msg = msg[:90] + "..."
	}
	return msg
}
