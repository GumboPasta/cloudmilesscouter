package airlines

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"

	"cloudmilesscouter/internal/scraper"
)

const (
	deltaHomeURL      = "https://www.delta.com/"
	deltaBookURL      = "https://www.delta.com/flight-search/book-a-flight"
	deltaFormTimeout  = 20 * time.Second
	deltaResultsWait  = 45 * time.Second
	deltaGridWait     = 25 * time.Second
	deltaGridSelector = "idp-flight-grid"
)

// deltaNoResultsRe matches the copy Delta shows for a route/date with no award
// space. A search that lands here is a valid empty result, not a failure.
// TODO: verify against a live no-availability page — this is the common phrasing,
// not confirmed against Delta's DOM.
var deltaNoResultsRe = regexp.MustCompile(`(?i)no flights|no award|no results|sold out|couldn't find|could not find`)

// DeltaExtractJS runs in the results page and pulls the flight/fare data out of
// the DOM — Delta's search-results page is server-rendered and carries no JSON
// payload, so this is the only source. It returns a JSON string.
const DeltaExtractJS = `() => {
  const clean = s => (s || '').replace(/\s+/g, ' ').trim();
  const grid = document.querySelector('idp-flight-grid');
  const headEl = document.querySelector('idp-search-results-head, idp-flight-context-info');
  const headText = clean(headEl && headEl.textContent);
  const routeM = headText.match(/\b([A-Z]{3})\b[^A-Za-z]{0,4}\b([A-Z]{3})\b/);
  const dateM = headText.match(/[A-Z][a-z]{2,8},\s+[A-Z][a-z]{2,9}\s+\d{1,2},\s+\d{4}/);
  const out = {
    source: 'delta.com/flightsearch/search-results DOM extraction',
    route: routeM ? { origin: routeM[1], destination: routeM[2] } : null,
    dateText: dateM ? dateM[0] : null,
    flightCount: 0,
    flights: [],
  };
  if (!grid) return JSON.stringify(out);
  const rows = [...grid.children].filter(d => d.querySelector('idp-mach-core-flight-card'));
  const cabinOrder = ['Main', 'Comfort', 'First'];
  out.flights = rows.map(row => {
    const card = row.querySelector('idp-mach-core-flight-card');
    const cardText = clean(card.textContent);
    const logoEl = row.querySelector('idp-mach-core-flight-card-segment-logo');
    const logo = clean(logoEl && logoEl.textContent);
    const flightNumbers = (logo.match(/[A-Z]{2}\s?\d+/g) || []).map(s => s.replace(/\s+/g, ''));
    const line = row.querySelector('.flight-line');
    const codes = [...line.querySelectorAll('.flight-stop__airport-code')]
      .map(e => clean(e.textContent)).filter(c => /^[A-Z]{3}$/.test(c));
    const stopsEl = line.querySelector('.flight-line__flight-stops');
    const stopText = clean(stopsEl && stopsEl.textContent);
    const sm = stopText.match(/(\d+)\s*Stop/i);
    const stops = sm ? +sm[1] : (/Nonstop/i.test(stopText) ? 0 : Math.max(0, codes.length - 2));
    const durM = cardText.match(/(\d+)h(?:\s*(\d+)m)?/);
    const durationMinutes = durM ? (+durM[1]) * 60 + (durM[2] ? +durM[2] : 0) : null;
    const depEl = line.querySelector('.flight-line__times-origin');
    const arrEl = line.querySelector('.flight-line__times-destination');
    const fares = [...row.querySelectorAll('idp-fare-cell-desktop-template')].map((c, i) => {
      const txt = clean(c.textContent);
      const av = /miles/i.test(txt) && !/not available/i.test(txt);
      const miles = txt.match(/([\d,]+)\s*miles/i);
      const tax = txt.match(/\+\s*\$?([\d,.]+)/);
      return {
        cabin: cabinOrder[i] || ('col' + i),
        available: av,
        miles: av && miles ? +miles[1].replace(/,/g, '') : 0,
        taxUSD: av && tax ? +tax[1].replace(/,/g, '') : 0,
      };
    });
    return {
      flightNumbers,
      depart: clean(depEl && depEl.textContent),
      arrive: clean(arrEl && arrEl.textContent),
      origin: codes[0] || null,
      destination: codes[codes.length - 1] || null,
      connectingCodes: codes.slice(1, -1),
      stops,
      durationMinutes,
      fares,
    };
  });
  out.flightCount = out.flights.length;
  return JSON.stringify(out);
}`

// ScrapeDelta runs one "Shop with Miles" award search on delta.com and returns
// the flight/fare data extracted from the rendered results page.
//
// Award pricing shows without a SkyMiles login. Delta runs Akamai Bot Manager
// (like United and American), so this must run headed. The persistent profile is
// only there to give Akamai a stable browser.
func ScrapeDelta(profileDir string, headless bool, params scraper.SearchParams) ([]byte, error) {
	start := time.Now()
	slog.Info("scrape started", "airline", "delta",
		"origin", params.Origin, "destination", params.Destination, "date", params.Date.Format("2006-01-02"))

	fail := func(err error) ([]byte, error) {
		slog.Error("scrape failed", "airline", "delta", "err", err)
		return nil, err
	}

	session, err := scraper.NewSession(headless, profileDir)
	if err != nil {
		return fail(err)
	}
	defer session.Close()
	page := session.Page

	if _, err := page.Goto(deltaHomeURL); err != nil {
		return fail(err)
	}
	if _, err := page.Goto(deltaBookURL); err != nil {
		return fail(err)
	}

	if err := fillDeltaSearchForm(page, params); err != nil {
		return fail(fmt.Errorf("fill search form: %w", err))
	}

	findButton := page.GetByRole(playwright.AriaRole("button"), playwright.PageGetByRoleOptions{Name: "Find Flights"})
	if err := findButton.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(float64(deltaFormTimeout.Milliseconds()))}); err != nil {
		return fail(err)
	}

	// The search lands on either the flexible-dates strip or straight on the
	// results grid; both share the cacheKeySuffix, so jump to the results page.
	if err := page.WaitForURL("**/flightsearch/**", playwright.PageWaitForURLOptions{
		Timeout: playwright.Float(float64(deltaResultsWait.Milliseconds())),
	}); err != nil {
		return fail(err)
	}
	if key := deltaCacheKey(page.URL()); key != "" {
		if _, err := page.Goto("https://www.delta.com/flightsearch/search-results?cacheKeySuffix=" + key); err != nil {
			return fail(err)
		}
	}

	// A route/date with no award space never renders the grid; Delta shows a
	// no-results message instead. Wait for whichever appears. If it was the
	// no-results message, DeltaExtractJS sees no grid and returns a valid
	// {"flights":[]} payload, which the worker stores as a success.
	grid := page.Locator(deltaGridSelector)
	if err := grid.Or(page.GetByText(deltaNoResultsRe)).First().WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(float64(deltaGridWait.Milliseconds())),
	}); err != nil {
		return fail(err)
	}
	// Let the fare cells finish rendering.
	page.WaitForTimeout(1500)

	result, err := page.Evaluate(DeltaExtractJS)
	if err != nil {
		return fail(err)
	}
	body, ok := result.(string)
	if !ok {
		return fail(fmt.Errorf("extractor returned %T, want string", result))
	}

	slog.Info("scrape succeeded", "airline", "delta", "bytes", len(body), "duration_ms", time.Since(start).Milliseconds())
	return []byte(body), nil
}

// deltaCacheKey pulls the cacheKeySuffix query value out of a Delta flightsearch
// URL, or returns "" if there isn't one.
func deltaCacheKey(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Query().Get("cacheKeySuffix")
}

// fillDeltaSearchForm drives delta.com's "Book a Flight" widget for a one-way
// award search: origin/destination via the airport overlay, trip type One Way,
// depart date, the "Shop with Miles" toggle on and "My Dates are Flexible" off.
func fillDeltaSearchForm(page playwright.Page, params scraper.SearchParams) error {
	timeout := playwright.Float(float64(deltaFormTimeout.Milliseconds()))
	clickOpts := playwright.LocatorClickOptions{Timeout: timeout}

	pickAirport := func(fieldLabel, code string) error {
		// Trigger button's aria-label is "<fieldLabel>" when empty or
		// "<fieldLabel>, DFW, Dallas-Fort Worth, TX" once a value is remembered.
		trigger := page.Locator(fmt.Sprintf("button[aria-label^=%q]", fieldLabel)).First()
		if err := trigger.Click(clickOpts); err != nil {
			return fmt.Errorf("open %s picker: %w", fieldLabel, err)
		}
		input := page.GetByRole(playwright.AriaRole("textbox"), playwright.PageGetByRoleOptions{Name: fieldLabel})
		if err := input.Fill(code, playwright.LocatorFillOptions{Timeout: timeout}); err != nil {
			return fmt.Errorf("type %s: %w", fieldLabel, err)
		}
		if err := page.GetByRole(playwright.AriaRole("option")).First().
			Click(clickOpts); err != nil {
			return fmt.Errorf("pick %s option: %w", fieldLabel, err)
		}
		return nil
	}

	if err := pickAirport("Origin", params.Origin); err != nil {
		return err
	}
	if err := pickAirport("Destination", params.Destination); err != nil {
		return err
	}

	// Trip type -> One Way. The trip-type control is a combobox whose accessible
	// name includes the current value ("Trip Type, Round Trip").
	tripType := page.Locator("[role=combobox][aria-label*='Trip Type'], [aria-label^='Trip Type']").First()
	if val, _ := tripType.GetAttribute("aria-label"); !strings.Contains(val, "One Way") {
		if err := tripType.Click(clickOpts); err != nil {
			return fmt.Errorf("open trip type: %w", err)
		}
		if err := page.GetByRole(playwright.AriaRole("option"), playwright.PageGetByRoleOptions{Name: "One Way"}).
			Click(clickOpts); err != nil {
			return fmt.Errorf("select One Way: %w", err)
		}
	}

	// Depart date. The calendar shows two months at a time; page forward with
	// the right-hand nav button until the target day's cell is visible. The day
	// cell is <button role="gridcell" data-date-value="MM-DD-YYYY">.
	if err := page.GetByRole(playwright.AriaRole("button"), playwright.PageGetByRoleOptions{Name: "Depart"}).
		First().Click(clickOpts); err != nil {
		return fmt.Errorf("open calendar: %w", err)
	}
	if err := selectDeltaDate(page, params.Date); err != nil {
		return err
	}
	// Closes the calendar. Best-effort: the scraper already tolerates landing on
	// the flexible-dates strip, and a stuck overlay would surface as the
	// "Find Flights" click timeout in ScrapeDelta.
	_ = page.GetByRole(playwright.AriaRole("button"), playwright.PageGetByRoleOptions{Name: "Done"}).
		Click(playwright.LocatorClickOptions{Timeout: playwright.Float(3000)})

	// "Shop with Miles" on — the one form step that must hard-fail. If it doesn't
	// engage, Delta runs a cash search: DeltaExtractJS gates availability on
	// /miles/i, so every fare comes back unavailable, 0 awards get stored as a
	// success, and the ETL wipes the route's previous good rows.
	if checked, _ := page.GetByLabel("Shop with Miles").IsChecked(); !checked {
		if err := page.GetByText("Shop with Miles").Click(clickOpts); err != nil {
			return fmt.Errorf("toggle Shop with Miles: %w", err)
		}
	}
	switch checked, err := page.GetByLabel("Shop with Miles").IsChecked(); {
	case err != nil:
		return fmt.Errorf("assert Shop with Miles engaged: %w", err)
	case !checked:
		return fmt.Errorf("Shop with Miles toggle did not engage")
	}

	// "My Dates are Flexible" off (Delta sometimes turns it on with Shop with
	// Miles). Best-effort: the scraper tolerates the flexible-dates strip.
	if checked, _ := page.GetByLabel("My Dates are Flexible").IsChecked(); checked {
		_ = page.GetByText("My Dates are Flexible").Click(clickOpts)
	}
	return nil
}

// selectDeltaDate pages Delta's two-month calendar forward until the target
// day's cell is on screen, then clicks it. The day cell is
// <button data-date-value="MM-DD-YYYY">; cells for months outside the visible
// window are in the DOM but hidden, so the ":visible" filter matters.
func selectDeltaDate(page playwright.Page, date time.Time) error {
	// Delta's data-date-value has no leading zeros: "12-5-2026", "9-1-2026".
	dateValue := date.Format("1-2-2006")
	day := page.Locator(fmt.Sprintf("[data-date-value=%q]", dateValue)).First()
	next := page.Locator(".date-picker__nav-button--right").First()

	if err := next.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateAttached, Timeout: playwright.Float(15000),
	}); err != nil {
		return fmt.Errorf("calendar did not open: %w", err)
	}

	// The calendar renders a few months up front and appends more each time the
	// forward button is pressed; page until the target day's cell exists.
	for i := 0; i < 12; i++ {
		if n, _ := day.Count(); n > 0 {
			break
		}
		if disabled, _ := next.IsDisabled(); disabled {
			break
		}
		if err := next.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(8000)}); err != nil {
			return fmt.Errorf("advance calendar toward %s: %w", date.Format("2006-01-02"), err)
		}
		page.WaitForTimeout(500)
	}
	if n, _ := day.Count(); n == 0 {
		return fmt.Errorf("calendar never rendered %s", date.Format("2006-01-02"))
	}
	// The cell can be in an off-screen month pane, so click it directly rather
	// than through Playwright's visibility checks.
	if _, err := day.Evaluate("el => el.click()", nil); err != nil {
		return fmt.Errorf("click date %s: %w", date.Format("2006-01-02"), err)
	}
	return nil
}

// HasResultsDelta reports whether an extracted Delta payload has any flights.
func HasResultsDelta(body []byte) (bool, error) {
	var resp struct {
		Flights []json.RawMessage `json:"flights"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return false, err
	}
	return len(resp.Flights) > 0, nil
}
