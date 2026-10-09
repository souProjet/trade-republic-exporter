// Package waf solves the AWS WAF challenge that guards the Trade Republic API
// by loading the web app in headless Chrome and reading the resulting token.
package waf

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

const (
	appURL        = "https://app.traderepublic.com/"
	userAgent     = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"
	challengeWait = 5 * time.Second
	timeout       = 45 * time.Second
)

// hideAutomation removes the navigator.webdriver flag, which the challenge
// script checks before issuing a token.
const hideAutomation = `Object.defineProperty(navigator, 'webdriver', { get: () => undefined })`

// Token returns an aws-waf-token. It requires Chrome or Chromium on the host.
func Token(ctx context.Context) (string, error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.UserAgent(userAgent),
	)

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()

	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()

	// The first action allocates the browser and must not carry the timeout,
	// otherwise the browser is torn down with it.
	if err := chromedp.Do(browserCtx, chromedp.Navigate("about:blank")); err != nil {
		return "", fmt.Errorf("start Chrome (is Chrome or Chromium installed?): %w", err)
	}

	runCtx, cancelRun := context.WithTimeout(browserCtx, timeout)
	defer cancelRun()

	err := chromedp.Do(runCtx,
		chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
			_, err := cdp.Call(ctx, t, page.AddScriptToEvaluateOnNewDocument,
				page.AddScriptToEvaluateOnNewDocumentParams{Source: hideAutomation})
			return err
		}),
		chromedp.Navigate(appURL),
		chromedp.Sleep(challengeWait),
	)
	if err != nil {
		return "", fmt.Errorf("load %s: %w", appURL, err)
	}

	token, err := chromedp.Run(runCtx, readToken)
	if err != nil {
		return "", fmt.Errorf("read WAF token: %w", err)
	}
	if token == "" {
		return "", fmt.Errorf("no aws-waf-token issued by %s", appURL)
	}
	return token, nil
}

// readToken prefers the cookie and falls back to the challenge script's own
// accessor, which some Chrome builds keep out of the cookie jar.
func readToken(ctx context.Context, t *chromedp.Target) (string, error) {
	cookies, err := cdp.Call(ctx, t, network.GetCookies, network.GetCookiesParams{})
	if err != nil {
		return "", err
	}
	for _, cookie := range cookies.Cookies {
		if strings.Contains(cookie.Name, "aws-waf-token") {
			return cookie.Value, nil
		}
	}

	const script = `(window.AWSWafIntegration && window.AWSWafIntegration.getToken && window.AWSWafIntegration.getToken()) || ""`
	token, err := chromedp.Evaluate[string](script)(ctx, t)
	if err != nil {
		return "", nil
	}
	return token, nil
}
