package airlines

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"

	"cloudmilesscouter/internal/mailotp"
	"cloudmilesscouter/internal/scraper"
)

const (
	unitedHomeURL     = "https://www.united.com/en/us"
	fetchFlightsGlob  = "**/api/flight/FetchFlights"
	chooseFlightsBase = "https://www.united.com/en/us/fsr/choose-flights"

	// unitedModalSelector matches the sign-in dialog's container. United's modal
	// carries no role="dialog"/aria-modal — it's a div.atm-c-modal__body — so
	// scope by that class first, with the ARIA variants as a hedge.
	unitedModalSelector = `.atm-c-modal__body, .atm-c-modal, [role="dialog"], [aria-modal="true"], dialog`
)

// unitedAirportNames maps IATA airport/metro codes to the "City, ST, US (XXX)"
// display strings United's choose-flights URL wants in its f/t params. The
// format (Title Case, multi-airport metros shown as "(All Airports)") is taken
// from United's own airport strings in
// testdata/samples/united_dfw_nyc_results.json. Anything not listed is passed
// through unchanged, so a full display string still works exactly as before and
// an unknown code is left for United to resolve.
//
// TODO: not yet confirmed against a live United search (the profile needs a
// re-bootstrap first). Verify the exact string United accepts and extend the
// table as routes get tested.
var unitedAirportNames = map[string]string{
	"BOS": "Boston, MA, US (BOS)",
	"CHI": "Chicago, IL, US (All Airports)",
	"DCA": "Washington, DC, US (DCA)",
	"DEN": "Denver, CO, US (DEN)",
	"DFW": "Dallas, TX, US (DFW)",
	"EWR": "Newark, NJ, US (EWR)",
	"FLL": "Fort Lauderdale, FL, US (FLL)",
	"IAD": "Washington, DC, US (IAD)",
	"IAH": "Houston, TX, US (IAH)",
	"JFK": "New York, NY, US (JFK)",
	"LAX": "Los Angeles, CA, US (LAX)",
	"LGA": "New York, NY, US (LGA)",
	"NYC": "New York, NY, US (All Airports)",
	"ORD": "Chicago, IL, US (ORD)",
	"SFO": "San Francisco, CA, US (SFO)",
	"WAS": "Washington, DC, US (All Airports)",
}

// unitedAirportName resolves an IATA code to United's display string, or returns
// the input unchanged if it isn't a known code (already a display string, or an
// airport not in the table yet).
func unitedAirportName(code string) string {
	if name, ok := unitedAirportNames[strings.ToUpper(strings.TrimSpace(code))]; ok {
		return name
	}
	return code
}

// BuildChooseFlightsURL builds the deep-link URL that triggers United's
// FetchFlights search, matching the pattern captured from a working manual
// search. Origin/Destination are IATA codes (e.g. "DFW", "NYC"), resolved to
// United's display-string format via unitedAirportName; a raw display string is
// also accepted.
func BuildChooseFlightsURL(p scraper.SearchParams) string {
	q := url.Values{}
	q.Set("f", unitedAirportName(p.Origin))
	q.Set("t", unitedAirportName(p.Destination))
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

// ensureLoggedIn handles United's "remembered account" step-up prompt: when the
// persistent profile's miles session has lapsed, United re-shows just a
// password field (it still knows the account, so no email/OTP step — the
// device stays trusted from bootstrap). If no password prompt is up within a
// few seconds, the session is still good and this is a no-op.
//
// The password is submitted by pressing Enter (see submitLoginStep), and the
// fill is followed by a check that the prompt actually went away.
func ensureLoggedIn(page playwright.Page, password string) error {
	pw := page.Locator(`#password`).First()
	if err := pw.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(6000),
	}); err != nil {
		return nil // no password prompt shown, session still valid
	}

	if password == "" {
		return errors.New("United prompted for password but UNITED_PASSWORD is not set")
	}
	if err := pw.Fill(password); err != nil {
		return fmt.Errorf("filling United password: %w", err)
	}
	if err := submitLoginStep(pw); err != nil {
		return fmt.Errorf("submitting United password: %w", err)
	}

	// Confirm the prompt cleared — a still-visible password field means the
	// login didn't take (wrong password, extra challenge), and the caller
	// should treat that as a failure rather than time out later on FetchFlights.
	if err := pw.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateHidden,
		Timeout: playwright.Float(15000),
	}); err != nil {
		return errors.New("United still showing the password prompt after sign-in (bad password or extra challenge)")
	}
	return nil
}

// unitedSignInSearchURL is a throwaway award search whose only purpose is to
// make United pop its sign-in modal (award/miles pricing is gated behind a
// login — see README). The route/date don't matter.
func unitedSignInSearchURL() string {
	return BuildChooseFlightsURL(scraper.SearchParams{
		Origin: "SFO", Destination: "JFK", Date: time.Now().AddDate(0, 1, 5),
	})
}

// Bootstrap opens a headed browser against profileDir (a persistent Chromium
// profile directory — the same one Scrape will reuse), navigates somewhere
// that triggers United's sign-in modal, and blocks on stdin: the human logs
// in, completes OTP, and checks "remember me" by hand, then presses Enter
// here. Because the profile is persistent, the login state is saved to disk
// automatically when the browser closes. No United credentials are read or
// stored by this codebase in this path.
func Bootstrap(profileDir string) error {
	slog.Info("bootstrap started", "airline", "united")

	session, err := scraper.NewSession(false, profileDir)
	if err != nil {
		slog.Error("bootstrap failed", "airline", "united", "err", err)
		return err
	}
	defer session.Close()

	if _, err := session.Page.Goto(unitedHomeURL); err != nil {
		slog.Error("bootstrap failed", "airline", "united", "err", err)
		return err
	}
	if _, err := session.Page.Goto(unitedSignInSearchURL()); err != nil {
		slog.Error("bootstrap failed", "airline", "united", "err", err)
		return err
	}

	fmt.Println(`A sign-in modal should appear. Log in manually, complete the OTP, and leave "Remember me" checked.`)
	fmt.Println(`If you see a "Continue shopping?" prompt first, click "Sign In" on it.`)
	fmt.Println("Once you're logged in (your name shows top-right), press Enter here...")
	bufio.NewReader(os.Stdin).ReadString('\n')

	if _, err := session.Page.Goto(unitedHomeURL); err != nil {
		slog.Error("bootstrap failed", "airline", "united", "err", err)
		return err
	}

	slog.Info("bootstrap succeeded", "airline", "united", "profile_dir", profileDir)
	return nil
}

// AutoBootstrap does the same one-time device-trust setup as Bootstrap with no
// human at the browser: it drives the sign-in modal itself, and when United
// emails a verification code, reads it via IMAP (internal/mailotp) instead of
// waiting on stdin. It runs headed so a run can be watched, but nothing in it
// needs a person to click.
//
// United's login is a modal reached from a search page, not a standalone URL:
//   - optional "Continue shopping?" interstitial → click "Sign In"
//   - either a remembered-account password field, or an email-first step
//     (fill email → "Continue" → password)
//   - "Sign in" → optional OTP step for a new device
//
// The email/password field ids (#MPIDEmailField, input[type=password]) and the
// interstitial were confirmed live; the OTP step's markup wasn't (it's behind
// a password submit). On any failure it saves a screenshot AND dumps the
// visible form controls to the log, so the next fix doesn't need a blind
// re-run.
func AutoBootstrap(profileDir, username, password string, gmail mailotp.Config) error {
	if username == "" || password == "" {
		return errors.New("AutoBootstrap needs UNITED_USERNAME and UNITED_PASSWORD set")
	}
	if gmail.Address == "" || gmail.AppPassword == "" {
		return errors.New("AutoBootstrap needs GMAIL_ADDRESS and GMAIL_APP_PASSWORD set")
	}

	slog.Info("auto-bootstrap started", "airline", "united")

	session, err := scraper.NewSession(false, profileDir)
	if err != nil {
		slog.Error("auto-bootstrap failed", "airline", "united", "err", err)
		return err
	}
	defer session.Close()

	page := session.Page
	fail := func(step string, err error) error {
		shot := profileDir + "-bootstrap-failure.png"
		if _, ssErr := page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String(shot)}); ssErr != nil {
			shot = "(screenshot failed)"
		}
		slog.Error("auto-bootstrap failed", "airline", "united",
			"step", step, "err", err, "screenshot", shot, "form_controls", describeForms(page))
		return fmt.Errorf("auto-bootstrap failed at %s: %w", step, err)
	}

	if _, err := page.Goto(unitedHomeURL); err != nil {
		return fail("goto home", err)
	}

	// United's SPA renders the sign-in modal anywhere from a few seconds to
	// ~40s after the search loads, so there's no reliable "is login needed?"
	// check up front. Instead: loop — trigger the search, and whichever comes
	// first wins. A FetchFlights payload means we're miles-authorized (done);
	// a sign-in modal means fill it and go round again.
	var body []byte
	deadline := time.Now().Add(3 * time.Minute)
	firstPass := true
	for time.Now().Before(deadline) {
		acted, herr := handleUnitedSignIn(page, username, password, gmail)
		if herr != nil {
			return fail("sign-in", herr)
		}

		trigger := func() error { return nil }
		switch {
		case firstPass:
			trigger = func() error { _, e := page.Goto(unitedSignInSearchURL()); return e }
		case !acted:
			// Nothing to fill and no FetchFlights yet — the modal may still be
			// rendering on the current page. Wait for it before re-navigating
			// (a re-nav would restart its render clock and thrash).
			if visibleWithin(page.Locator(`#password, #MPIDEmailField`).First(), 10*time.Second) {
				continue
			}
			trigger = func() error { _, e := page.Goto(unitedSignInSearchURL()); return e }
		}
		firstPass = false

		if b, cerr := scraper.CaptureJSONResponse(page, fetchFlightsGlob, 25*time.Second, trigger); cerr == nil {
			body = b
			break
		}
	}
	if body == nil {
		return fail("verify", errors.New("FetchFlights never returned within 3m — sign-in isn't clearing the miles gate"))
	}

	slog.Info("auto-bootstrap succeeded — profile is logged in and miles-authorized",
		"airline", "united", "profile_dir", profileDir, "verify_bytes", len(body))
	return nil
}

// handleUnitedSignIn deals with whatever United is showing to gate the award
// search right now: the "Continue shopping?" interstitial, an email-first step,
// or the password step-up (reading an emailed OTP via gmail if a new device is
// challenged). acted is true if it interacted with something; acted=false with
// nil err means no sign-in UI is currently visible.
func handleUnitedSignIn(page playwright.Page, username, password string, gmail mailotp.Config) (acted bool, err error) {
	dialog := page.Locator(unitedModalSelector).First()
	gate := page.GetByText(regexp.MustCompile(`(?i)signed-in to see flight results with miles`))
	pwField := page.Locator(`#password`).First()
	emailField := page.Locator(`#MPIDEmailField, input[name="MileagePlusLogin.MPIDEmailField"]`).First()

	// "Continue shopping?" gate — a button-only modal, no field to anchor to.
	if visibleWithin(gate, time.Second) && !visibleWithin(pwField, 500*time.Millisecond) {
		if btn, e := firstVisible([]playwright.Locator{
			dialog.Locator(`button`).Filter(playwright.LocatorFilterOptions{HasText: regexp.MustCompile(`(?i)^\s*sign ?in\s*$`)}),
		}, 4*time.Second); e == nil {
			_ = btn.Click()
		}
		return true, nil
	}

	// Email-first step (fresh profile).
	if visibleWithin(emailField, time.Second) && !visibleWithin(pwField, 500*time.Millisecond) {
		if e := emailField.Fill(username); e != nil {
			return true, fmt.Errorf("fill email: %w", e)
		}
		if e := submitLoginStep(emailField); e != nil {
			return true, fmt.Errorf("submit email step: %w", e)
		}
		if !visibleWithin(pwField, 15*time.Second) {
			return true, errors.New("password field never appeared after email step")
		}
	}

	// Password step-up.
	if visibleWithin(pwField, time.Second) {
		if e := pwField.Fill(password); e != nil {
			return true, fmt.Errorf("fill password: %w", e)
		}
		sentAt := time.Now()
		if e := submitLoginStep(pwField); e != nil {
			return true, fmt.Errorf("submit password: %w", e)
		}
		if e := submitUnitedOTP(page, dialog, gmail, sentAt); e != nil {
			return true, e
		}
		return true, nil
	}

	return false, nil
}

// submitLoginStep submits the login form that contains field by pressing Enter
// (implicit form submission — fires both the form's submit event and the submit
// button's activation), and if the step doesn't advance within a few seconds,
// falls back to clicking the form's submit button. United's login buttons have
// no stable id and the modal re-renders on input, so Enter is the reliable path.
func submitLoginStep(field playwright.Locator) error {
	_ = field.Press("Enter")
	if err := field.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateHidden, Timeout: playwright.Float(6000),
	}); err == nil {
		return nil // step advanced
	}
	form := field.Locator(`xpath=ancestor::form[1]`)
	btn, err := firstVisible([]playwright.Locator{
		form.Locator(`button[type="submit"].atm-c-btn--primary`),
		form.Locator(`button[type="submit"]`),
	}, 5*time.Second)
	if err != nil {
		return fmt.Errorf("Enter did not advance the step and no submit button was found: %w", err)
	}
	return btn.Click()
}

// submitUnitedOTP fills and submits the email verification code if United
// challenged this device; a no-op (nil) if no OTP prompt appears.
func submitUnitedOTP(page playwright.Page, dialog playwright.Locator, gmail mailotp.Config, sentAt time.Time) error {
	otpField, e := firstVisible([]playwright.Locator{
		dialog.Locator(`input[autocomplete="one-time-code"]`),
		dialog.Locator(`input[id*="Pin" i], input[id*="Code" i], input[name*="Pin" i], input[name*="Code" i]`),
		dialog.GetByRole(playwright.AriaRole("textbox"), playwright.LocatorGetByRoleOptions{Name: regexp.MustCompile(`(?i)verification|one-time|passcode|security code`)}),
	}, 12*time.Second)
	if e != nil {
		slog.Info("no OTP prompt — device still trusted", "airline", "united")
		return nil
	}

	slog.Info("united OTP prompt shown, reading code from email", "airline", "united")
	code, ce := mailotp.WaitForCode(gmail, "united", sentAt, 120*time.Second)
	if ce != nil {
		return ce
	}
	if fe := otpField.Fill(code); fe != nil {
		return fmt.Errorf("fill OTP: %w", fe)
	}
	if remember, re := firstVisible([]playwright.Locator{
		dialog.GetByRole(playwright.AriaRole("checkbox"), playwright.LocatorGetByRoleOptions{Name: regexp.MustCompile(`(?i)remember|trust`)}),
		dialog.Locator(`#rememberMe, input[name*="ememberMe" i]`),
	}, 3*time.Second); re == nil {
		if checked, _ := remember.IsChecked(); !checked {
			_ = remember.Check()
		}
	}
	if ce := submitLoginStep(otpField); ce != nil {
		return fmt.Errorf("submit OTP: %w", ce)
	}
	return nil
}

// visibleWithin reports whether loc becomes visible within timeout.
func visibleWithin(loc playwright.Locator, timeout time.Duration) bool {
	return loc.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible, Timeout: playwright.Float(float64(timeout.Milliseconds())),
	}) == nil
}

// firstVisible polls candidates until one becomes visible (returning that exact
// element) or timeout elapses. A candidate that matches several elements is
// walked entry by entry, so a broad locator can be handed in without tripping
// Playwright strict mode.
func firstVisible(candidates []playwright.Locator, timeout time.Duration) (playwright.Locator, error) {
	deadline := time.Now().Add(timeout)
	for {
		for _, c := range candidates {
			n, err := c.Count()
			if err != nil {
				continue
			}
			for i := 0; i < n; i++ {
				el := c.Nth(i)
				if visible, err := el.IsVisible(); err == nil && visible {
					return el, nil
				}
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("none of %d candidate locators became visible within %s", len(candidates), timeout)
		}
		time.Sleep(400 * time.Millisecond)
	}
}

// describeForms returns a compact one-line dump of the page's visible inputs,
// buttons and links — logged on failure so selector fixes don't need a blind
// re-run. Best-effort: returns "" if the page can't be evaluated.
func describeForms(page playwright.Page) string {
	const js = `() => {
		const vis = el => !!(el.offsetWidth || el.offsetHeight);
		// No el.value: a filled #password / #MPIDEmailField would otherwise put the
		// typed MileagePlus password or username straight into the failure log.
		// United's real buttons are <button> elements, so el.innerText covers them.
		const desc = el => [el.tagName, el.type||'', el.id||'', el.name||'',
			el.getAttribute('aria-label')||'', el.placeholder||'',
			(el.innerText||'').trim().slice(0,30)]
			.filter(Boolean).join('|');
		return [...document.querySelectorAll('input,button,a[role="button"]')]
			.filter(vis).map(desc).join('  //  ');
	}`
	v, err := page.Evaluate(js)
	if err != nil {
		return "(evaluate failed: " + err.Error() + ")"
	}
	s, _ := v.(string)
	return s
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
