package airlines

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/playwright-community/playwright-go"

	"cloudmilesscouter/internal/scraper"
)

const (
	alaskaResultsBase = "https://www.alaskaair.com/search/results"
	alaskaResultsWait = 25 * time.Second
	alaskaCardSel     = ".flight-card-content"
)

// BuildAlaskaResultsURL builds the deep-link that runs an Alaska award ("Use
// points") search directly — no form to drive. Origin/Destination are IATA
// codes; date is the departure day.
func BuildAlaskaResultsURL(p scraper.SearchParams) string {
	q := url.Values{}
	q.Set("O", p.Origin)
	q.Set("D", p.Destination)
	q.Set("OD", p.Date.Format("2006-01-02"))
	q.Set("A", "1")
	q.Set("RT", "false")
	q.Set("ShoppingMethod", "onlineaward")
	q.Set("locale", "en-us")
	return alaskaResultsBase + "?" + q.Encode()
}

// AlaskaExtractJS pulls the flight/fare data out of Alaska's Svelte results grid
// (the page has no JSON payload). It returns a JSON string. Connecting
// itineraries render as "Multiple flights" with no per-segment number.
const AlaskaExtractJS = `() => {
  const clean = s => (s || '').replace(/\s+/g, ' ').trim();
  const cards = [...document.querySelectorAll('.flight-card-content')];
  const routeEl = [...document.querySelectorAll('h1,h2,h3,[class*="route"]')]
    .find(e => /\([A-Z]{3}\)\s*to\s/i.test(clean(e.textContent)) && e.children.length < 6);
  const codes = routeEl ? (clean(routeEl.textContent).match(/\(([A-Z]{3})\)/g) || []).map(s => s.slice(1, 4)) : [];
  const out = {
    source: 'alaskaair.com/search/results DOM extraction',
    route: codes.length >= 2 ? { origin: codes[0], destination: codes[1] } : null,
    flightCount: 0,
    flights: [],
  };
  out.flights = cards.map(c => {
    const q = s => c.querySelector(s);
    const qa = s => [...c.querySelectorAll(s)];
    const fnRaw = clean(q('.flight-number') && q('.flight-number').textContent);
    const dur = clean(q('.duration') && q('.duration').textContent);
    const dm = dur.match(/(\d+)h(?:\s*(\d+)m)?/);
    const nd = clean(q('.duration-next-day') && q('.duration-next-day').textContent).match(/\+(\d+)\s*day/);
    // Stop count comes from the details section's data-testid
    // ("flight-details-<i>-stops-<n>"); the connection airport(s) come from the
    // path widget's text ("PDX 8h 2m"), found by its aria-label rather than a
    // utility class. Fall back to parsing the path text if the testid is gone.
    const stopsEl = q('[data-testid*="-stops-"]');
    const stopsTid = stopsEl ? (stopsEl.getAttribute('data-testid') || '') : '';
    const pathEl = q('[aria-label*="flight path"], [aria-label*="Flight path"]');
    const stopText = clean(pathEl && pathEl.textContent);
    const via = (stopText.match(/\b[A-Z]{3}\b/g) || []);
    const stopM = stopsTid.match(/-stops-(\d+)/);
    const stops = stopM ? +stopM[1] : (/nonstop/i.test(stopText) ? 0 : (via.length || 1));
    const airportCodes = qa('.airport-code').map(e => clean(e.textContent));
    // A fare is a <button> whose text carries both a points amount and a $
    // tax — version-agnostic (the tile component has been "fare-tile",
    // "valuetile" and "fare-tile-v2" across redesigns; the anonymous and
    // signed-in pages don't always render the same one). Cabin comes from the
    // "fare-tile--<cabin>" modifier class when present, else the leading word.
    const fares = qa('button')
      .filter(b => /\bpoints\b/i.test(b.textContent) && /\$\s?\d/.test(b.textContent))
      .map(b => {
        const t = clean(b.textContent);
        const clsCabin = (String(b.className).match(/fare-tile--([a-z]+)/i) || [])[1];
        const cabin = clsCabin
          ? clsCabin[0].toUpperCase() + clsCabin.slice(1)
          : (t.match(/\b(Saver|Main|First|Business)\b/i) || ['', 'Main'])[1];
        const pts = t.match(/([\d,.]+)\s*k?\s*points/i);
        const tax = t.match(/\+\s*\$\s?([\d,.]+)/);
        let miles = 0;
        if (pts) {
          miles = parseFloat(pts[1].replace(/,/g, ''));
          if (/k\s*points/i.test(t)) miles *= 1000;
        }
        return { cabin, miles: Math.round(miles), taxUSD: tax ? parseFloat(tax[1].replace(/,/g, '')) : 0 };
      });
    return {
      flightNumber: fnRaw,
      via,
      depart: clean(q('.departure-time') && q('.departure-time').textContent),
      arrive: clean(q('.arrival-time') && q('.arrival-time').textContent),
      durationMinutes: dm ? (+dm[1]) * 60 + (dm[2] ? +dm[2] : 0) : null,
      arrivesNextDay: nd ? +nd[1] : 0,
      origin: airportCodes[0] || null,
      destination: airportCodes[airportCodes.length - 1] || null,
      stops,
      fares,
    };
  });
  out.flightCount = out.flights.length;
  return JSON.stringify(out);
}`

// ScrapeAlaska runs one award search on alaskaair.com and returns the flight
// data extracted from the rendered results page. Alaska's award search is
// anonymous and deep-linkable, so there is no form to fill and no login. Run
// headed unless a live check proves headless works.
func ScrapeAlaska(profileDir string, headless bool, params scraper.SearchParams) ([]byte, error) {
	start := time.Now()
	slog.Info("scrape started", "airline", "alaska",
		"origin", params.Origin, "destination", params.Destination, "date", params.Date.Format("2006-01-02"))

	fail := func(err error) ([]byte, error) {
		slog.Error("scrape failed", "airline", "alaska", "err", err)
		return nil, err
	}

	session, err := scraper.NewSession(headless, profileDir)
	if err != nil {
		return fail(err)
	}
	defer session.Close()
	page := session.Page

	if _, err := page.Goto(BuildAlaskaResultsURL(params)); err != nil {
		return fail(err)
	}

	// A normal search renders flight cards within a few seconds. A route/date
	// with no award space renders none — and so does a page that stalled or got
	// walled. Wait for a card to become visible; on a timeout, give the DOM a
	// few more seconds and hand off to the extractor regardless. AlaskaExtractJS
	// returns {"flights":[]} when there are no cards, which the worker stores as
	// a valid empty result — and its HasResults check then warns, the signal
	// that a selector may have drifted.
	//
	// (Do NOT race the card against a `no flights`-style text locator: Alaska's
	// results page always carries a hidden "no flights eligible for your
	// selected upgrade type" <p>, and `card.Or(text).First()` locks onto that
	// hidden element and times out even when results are present.)
	card := page.Locator(alaskaCardSel).First()
	if err := card.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(float64(alaskaResultsWait.Milliseconds())),
	}); err != nil {
		slog.Warn("alaska: no flight card became visible; extracting whatever rendered",
			"airline", "alaska", "err", err)
		page.WaitForTimeout(4000)
	} else {
		// The card skeleton renders a beat before its fare tiles / points
		// prices, so wait for a price to appear before extracting (best effort —
		// a genuinely empty result never shows one).
		_ = page.Locator(`[data-testid="award-price"]`).First().WaitFor(playwright.LocatorWaitForOptions{
			State:   playwright.WaitForSelectorStateVisible,
			Timeout: playwright.Float(10000),
		})
		page.WaitForTimeout(1500)
	}

	result, err := page.Evaluate(AlaskaExtractJS)
	if err != nil {
		return fail(err)
	}
	body, ok := result.(string)
	if !ok {
		return fail(fmt.Errorf("extractor returned %T, want string", result))
	}

	slog.Info("scrape succeeded", "airline", "alaska", "bytes", len(body), "duration_ms", time.Since(start).Milliseconds())
	return []byte(body), nil
}

// HasResultsAlaska reports whether an extracted Alaska payload has any flights.
func HasResultsAlaska(body []byte) (bool, error) {
	var resp struct {
		Flights []json.RawMessage `json:"flights"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return false, err
	}
	return len(resp.Flights) > 0, nil
}
