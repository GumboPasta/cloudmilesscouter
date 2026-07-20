package airlines

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"

	"cloudmilesscouter/internal/scraper"
)

const (
	unitedHomeURL     = "https://www.united.com/en/us"
	unitedLoginURL    = "https://www.united.com/en/us/mileageplus/login"
	fetchFlightsGlob  = "**/api/flight/FetchFlights"
	chooseFlightsBase = "https://www.united.com/en/us/fsr/choose-flights"
)

// BuildChooseFlightsURL builds the deep-link URL that triggers United's
// FetchFlights search, matching the pattern captured from a working manual
// search. Origin/Destination must already be in United's display-string
// format (e.g. "DALLAS, TX, US (ALL AIRPORTS)") — an IATA-code lookup is a
// future enhancement, not built here.
func BuildChooseFlightsURL(p scraper.SearchParams) string {
	q := url.Values{}
	q.Set("f", p.Origin)
	q.Set("t", p.Destination)
	q.Set("d", p.Date.Format("2006-01-02"))
	q.Set("tt", "1")
	q.Set("at", "1")
	q.Set("sc", "7")
	q.Set("px", "1")
	q.Set("pst", "r/o=-S-W")
	q.Set("taxng", "1")
	q.Set("newHP", "True")
	q.Set("clm", "7")
	q.Set("st", "bestmatches")
	q.Set("tqp", "A")

	// url.Values.Encode() uses "+" for spaces and escapes "(" / ")" as %28/%29
	// (application/x-www-form-urlencoded rules). The real browser-generated URL
	// we captured uses %20 for spaces and leaves parentheses literal — matching
	// that exact byte pattern avoids looking like a hand-crafted request to
	// United's WAF, which appears to reset the connection otherwise.
	raw := q.Encode()
	raw = strings.ReplaceAll(raw, "+", "%20")
	raw = strings.ReplaceAll(raw, "%28", "(")
	raw = strings.ReplaceAll(raw, "%29", ")")

	return chooseFlightsBase + "?" + raw
}

// ensureLoggedIn checks whether United is showing its "remembered account"
// password prompt (the persistent profile already knows the account, so no
// email/OTP step appears — only a password field, confirmed via Playwright
// codegen against a live session). If the prompt isn't present, it's a no-op.
func ensureLoggedIn(page playwright.Page, password string) error {
	passwordField := page.GetByRole(playwright.AriaRole("textbox"), playwright.PageGetByRoleOptions{
		Name: "Password",
	})

	if err := passwordField.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(5000),
	}); err != nil {
		return nil // no password prompt shown, already logged in
	}

	if password == "" {
		return errors.New("United prompted for password but UNITED_PASSWORD is not set")
	}

	if err := passwordField.Fill(password); err != nil {
		return err
	}

	signInButton := page.GetByLabel("Sign in for the best").GetByRole(playwright.AriaRole("button"), playwright.LocatorGetByRoleOptions{
		Name:  "Sign in",
		Exact: playwright.Bool(true),
	})
	return signInButton.Click()
}

// Bootstrap opens a headed browser against profileDir (a persistent Chromium
// profile directory — the same one Scrape will reuse) at United's login page,
// and blocks on stdin: the human logs in, completes OTP, and checks "remember
// this device" by hand, then presses Enter here. Because the profile is
// persistent, the resulting login state is saved to disk automatically when
// the browser closes — no explicit export/import step needed. No United
// credentials are ever read or stored by this codebase.
func Bootstrap(profileDir string) error {
	slog.Info("bootstrap started", "airline", "united")

	session, err := scraper.NewSession(false, profileDir)
	if err != nil {
		slog.Error("bootstrap failed", "airline", "united", "err", err)
		return err
	}
	defer session.Close()

	// Visit the homepage first so United's tracking/Akamai cookies get set the
	// way they would for a real visitor, instead of deep-linking cold into login.
	if _, err := session.Page.Goto(unitedHomeURL); err != nil {
		slog.Error("bootstrap failed", "airline", "united", "err", err)
		return err
	}

	if _, err := session.Page.Goto(unitedLoginURL); err != nil {
		slog.Error("bootstrap failed", "airline", "united", "err", err)
		return err
	}

	fmt.Println(`Log in to United manually in the opened browser window, complete the OTP, and check "Remember this device".`)
	fmt.Println("Wait until you can see you're fully logged in (e.g. your name greeting on the homepage), then press Enter here...")
	bufio.NewReader(os.Stdin).ReadString('\n')

	// Reload the homepage so any trailing "remember this device" cookie writes
	// finish syncing before we close the profile.
	if _, err := session.Page.Goto(unitedHomeURL); err != nil {
		slog.Error("bootstrap failed", "airline", "united", "err", err)
		return err
	}

	slog.Info("bootstrap succeeded", "airline", "united", "profile_dir", profileDir)
	return nil
}

// Scrape runs one award search for United, reusing the persistent profile
// directory created by Bootstrap, navigates to the deep-link URL for params,
// auto-fills the password if the session has expired (the profile still
// remembers the account, so no email/OTP step ever appears), waits for the
// FetchFlights response, and returns its raw JSON body.
func Scrape(profileDir string, headless bool, params scraper.SearchParams, password string) ([]byte, error) {
	start := time.Now()
	slog.Info("scrape started", "airline", "united",
		"origin", params.Origin, "destination", params.Destination, "date", params.Date.Format("2006-01-02"))

	if _, err := os.Stat(profileDir); err != nil {
		err = fmt.Errorf("no United profile at %q — run `bootstrap` first: %w", profileDir, err)
		slog.Error("scrape failed", "airline", "united", "err", err)
		return nil, err
	}

	session, err := scraper.NewSession(headless, profileDir)
	if err != nil {
		slog.Error("scrape failed", "airline", "united", "err", err)
		return nil, err
	}
	defer session.Close()

	searchURL := BuildChooseFlightsURL(params)

	if _, err := session.Page.Goto(searchURL); err != nil {
		slog.Error("scrape failed", "airline", "united", "err", err)
		return nil, err
	}

	if err := ensureLoggedIn(session.Page, password); err != nil {
		slog.Error("scrape failed", "airline", "united", "err", err)
		return nil, err
	}

	body, err := scraper.CaptureJSONResponse(session.Page, fetchFlightsGlob, 30*time.Second, func() error {
		_, err := session.Page.Goto(searchURL)
		return err
	})
	if err != nil {
		slog.Error("scrape failed", "airline", "united", "err", err)
		return nil, err
	}

	slog.Info("scrape succeeded", "airline", "united", "bytes", len(body), "duration_ms", time.Since(start).Milliseconds())
	return body, nil
}

// HasResults reports whether a FetchFlights response body contains any
// flights. United returns HTTP 200 with an empty Trips[].Flights array (plus
// an Errors entry like "No flights found") when a route/date has no award
// availability — that's valid data, not a scrape failure, so callers should
// log it rather than treat it as an error.
func HasResults(body []byte) (bool, error) {
	var resp struct {
		Data struct {
			Trips []struct {
				Flights []json.RawMessage `json:"Flights"`
			} `json:"Trips"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return false, err
	}
	for _, trip := range resp.Data.Trips {
		if len(trip.Flights) > 0 {
			return true, nil
		}
	}
	return false, nil
}
