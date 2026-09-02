package scraper

import (
	"fmt"
	"time"

	"github.com/playwright-community/playwright-go"
)

// SearchParams is the shared shape every airline's search takes.
// Airline-specific display-string formatting happens in that airline's file.
type SearchParams struct {
	Origin      string
	Destination string
	Date        time.Time
}

// Session wraps one Playwright run: process, persistent context, page.
type Session struct {
	pw      *playwright.Playwright
	Context playwright.BrowserContext
	Page    playwright.Page
}

// NewSession launches Chromium against a persistent on-disk profile directory
// (profileDir), the same one used for every call (bootstrap and scrape alike).
// Reusing a real Chromium profile — not just replaying saved cookies into a
// fresh one — is what makes "remember this device" style trust actually
// stick with bot-risk engines like Akamai, since they weigh profile/session
// continuity beyond cookie contents. An empty/nonexistent profileDir just
// starts a fresh profile, which is fine for the very first bootstrap run.
func NewSession(headless bool, profileDir string) (*Session, error) {
	pw, err := playwright.Run()
	if err != nil {
		return nil, err
	}

	context, err := pw.Chromium.LaunchPersistentContext(profileDir, playwright.BrowserTypeLaunchPersistentContextOptions{
		Headless: playwright.Bool(headless),
		// United's Akamai Bot Manager checks navigator.webdriver, which Chromium
		// sets to true by default under automation control.
		Args: []string{"--disable-blink-features=AutomationControlled"},
	})
	if err != nil {
		pw.Stop()
		return nil, err
	}

	var page playwright.Page
	if pages := context.Pages(); len(pages) > 0 {
		page = pages[0]
	} else {
		page, err = context.NewPage()
		if err != nil {
			context.Close()
			pw.Stop()
			return nil, err
		}
	}

	return &Session{pw: pw, Context: context, Page: page}, nil
}

// Close tears down page/context/browser/playwright process. For a persistent
// context this also flushes the profile directory to disk.
func (s *Session) Close() {
	if s.Context != nil {
		s.Context.Close()
	}
	if s.pw != nil {
		s.pw.Stop()
	}
}

// CaptureJSONResponse calls trigger (e.g. a page navigation) and blocks until a
// response matching urlGlob arrives or timeout elapses, returning its raw body.
func CaptureJSONResponse(page playwright.Page, urlGlob string, timeout time.Duration, trigger func() error) ([]byte, error) {
	resp, err := page.ExpectResponse(urlGlob, trigger, playwright.PageExpectResponseOptions{
		Timeout: playwright.Float(float64(timeout.Milliseconds())),
	})
	if err != nil {
		return nil, err
	}
	if !resp.Ok() {
		return nil, fmt.Errorf("unexpected response status %d %q for %s", resp.Status(), resp.StatusText(), urlGlob)
	}
	return resp.Body()
}
