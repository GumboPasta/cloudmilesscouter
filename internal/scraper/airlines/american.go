package airlines

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"

	"cloudmilesscouter/internal/scraper"
)

const (
	americanHomeURL         = "https://www.aa.com/"
	americanFindFlightsURL  = "https://www.aa.com/booking/find-flights"
	americanResultsGlob     = "**/booking/choose-flights/**"
	americanFormTimeout     = 20 * time.Second
	americanResultsWaitTime = 45 * time.Second
)

// ScrapeAmerican runs one award search on aa.com and returns the raw JSON blob
// American's Angular app embeds in the results page as <script id="ng-state">.
//
// No login is required for award pricing, so unlike United there is no bootstrap
// step — the persistent profile only exists to give Akamai Bot Manager a stable
// browser to trust. A missing profileDir just starts a fresh profile. Akamai
// hard-blocks headless Chromium, so this must run headed (headless=false).
//
// The search form's URL query params do not pre-fill a cold session, so the
// fields are driven directly: trip type → one way, origin/destination (IATA),
// depart date (MM/DD/YYYY), the "Redeem miles" toggle, then Search. Clicking
// Search is a full-page navigation to the server-rendered results page.
func ScrapeAmerican(profileDir string, headless bool, params scraper.SearchParams) ([]byte, error) {
	start := time.Now()
	slog.Info("scrape started", "airline", "american",
		"origin", params.Origin, "destination", params.Destination, "date", params.Date.Format("2006-01-02"))

	fail := func(err error) ([]byte, error) {
		slog.Error("scrape failed", "airline", "american", "err", err)
		return nil, err
	}

	session, err := scraper.NewSession(headless, profileDir)
	if err != nil {
		return fail(err)
	}
	defer session.Close()
	page := session.Page

	// Visit the homepage first so Akamai's cookies get set the way they would
	// for a real visitor, instead of deep-linking cold into the search form.
	if _, err := page.Goto(americanHomeURL); err != nil {
		return fail(err)
	}
	if _, err := page.Goto(americanFindFlightsURL); err != nil {
		return fail(err)
	}

	if err := fillAmericanSearchForm(page, params); err != nil {
		return fail(fmt.Errorf("fill search form: %w", err))
	}

	searchButton := page.GetByRole(playwright.AriaRole("button"), playwright.PageGetByRoleOptions{
		Name:  "Search",
		Exact: playwright.Bool(true),
	})
	if err := searchButton.Click(); err != nil {
		return fail(err)
	}

	if err := page.WaitForURL(americanResultsGlob, playwright.PageWaitForURLOptions{
		Timeout: playwright.Float(float64(americanResultsWaitTime.Milliseconds())),
	}); err != nil {
		return fail(err)
	}

	ngState := page.Locator("#ng-state")
	if err := ngState.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateAttached,
		Timeout: playwright.Float(float64((10 * time.Second).Milliseconds())),
	}); err != nil {
		return fail(err)
	}
	body, err := ngState.TextContent()
	if err != nil {
		return fail(err)
	}

	slog.Info("scrape succeeded", "airline", "american", "bytes", len(body), "duration_ms", time.Since(start).Milliseconds())
	return []byte(body), nil
}

// fillAmericanSearchForm drives the aa.com "Book flights" form for a one-way
// award search. Filling the airport inputs by value is enough — the form's
// Angular model accepts a bare IATA code without an explicit autocomplete pick.
// Each control is only changed if it isn't already in the wanted state, because
// the persistent profile remembers the previous search's form values.
func fillAmericanSearchForm(page playwright.Page, params scraper.SearchParams) error {
	timeout := playwright.Float(float64(americanFormTimeout.Milliseconds()))
	clickOpts := playwright.LocatorClickOptions{Timeout: timeout}
	fillOpts := playwright.LocatorFillOptions{Timeout: timeout}

	// The form is Angular-rendered after the document loads; wait for a field to
	// exist before driving it.
	origin := page.Locator("#matOriginAirport")
	if err := origin.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(float64((40 * time.Second).Milliseconds())),
	}); err != nil {
		return fmt.Errorf("wait for form: %w", err)
	}

	// Trip type (mat-select#trip-type): ensure "One way" so no return date is
	// required. It may already be "One way" from a previous run.
	tripType := page.Locator("#trip-type")
	current, err := tripType.TextContent(playwright.LocatorTextContentOptions{Timeout: timeout})
	if err != nil {
		return fmt.Errorf("read trip type: %w", err)
	}
	if !strings.Contains(current, "One way") {
		if err := tripType.Click(clickOpts); err != nil {
			return fmt.Errorf("open trip-type menu: %w", err)
		}
		if err := page.GetByRole(playwright.AriaRole("option"), playwright.PageGetByRoleOptions{
			Name: "One way", Exact: playwright.Bool(true),
		}).Click(clickOpts); err != nil {
			return fmt.Errorf("select one way: %w", err)
		}
	}

	if err := origin.Fill(params.Origin, fillOpts); err != nil {
		return fmt.Errorf("origin: %w", err)
	}
	if err := page.Locator("#matDestinationAirport").Fill(params.Destination, fillOpts); err != nil {
		return fmt.Errorf("destination: %w", err)
	}

	date := params.Date.Format("01/02/2006")
	datePicker := page.Locator("#matOneWayDatePicker")
	if n, _ := datePicker.Count(); n == 0 {
		datePicker = page.Locator("#matDepartureDatePicker")
	}
	if err := datePicker.Fill(date, fillOpts); err != nil {
		return fmt.Errorf("depart date: %w", err)
	}
	// Dismiss the date-picker calendar popover so it doesn't cover the button.
	if err := page.Keyboard().Press("Escape"); err != nil {
		return fmt.Errorf("close calendar: %w", err)
	}

	// "Redeem miles" toggle — click the label only if the checkbox isn't already
	// checked (clicking always flips it).
	if checked, err := page.Locator("#redeem-miles").IsChecked(); err != nil {
		return fmt.Errorf("read redeem-miles: %w", err)
	} else if !checked {
		if err := page.GetByText("Redeem miles").Click(clickOpts); err != nil {
			return fmt.Errorf("redeem miles toggle: %w", err)
		}
	}
	return nil
}

// HasResultsAmerican reports whether an ng-state blob contains any itineraries.
// American returns a results page with an empty slices array (and often a
// populated itineraryResult.error) when a route/date has no award availability —
// valid data, not a scrape failure, so callers should log it rather than error.
func HasResultsAmerican(body []byte) (bool, error) {
	var resp struct {
		SearchData struct {
			ItineraryResult struct {
				Slices []json.RawMessage `json:"slices"`
			} `json:"itineraryResult"`
		} `json:"SearchData"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return false, err
	}
	return len(resp.SearchData.ItineraryResult.Slices) > 0, nil
}
