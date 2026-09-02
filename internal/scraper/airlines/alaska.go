package airlines

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"time"

	"github.com/playwright-community/playwright-go"

	"cloudmilesscouter/internal/scraper"
)

const (
	alaskaResultsBase = "https://www.alaskaair.com/search/results"
	alaskaResultsWait = 25 * time.Second
	alaskaCardSel     = ".flight-card-content"
)

// alaskaNoResultsRe matches the copy Alaska shows for a route/date with no award
// space. A search that lands here is a valid empty result, not a failure.
// TODO: verify against a live no-availability page — this is the common phrasing,
// not confirmed against Alaska's DOM.
var alaskaNoResultsRe = regexp.MustCompile(`(?i)no flights|no award|no results|sold out|couldn't find|could not find`)

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
    const stopText = clean(q('.relative') && q('.relative').textContent);
    const via = (stopText.match(/[A-Z]{3}/g) || []);
    const stops = /nonstop/i.test(stopText) ? 0 : (via.length || 1);
    const airportCodes = qa('.airport-code').map(e => clean(e.textContent));
    const fares = qa('button')
      .map(b => clean(b.textContent))
      .filter(t => /points/i.test(t) && t.length < 90)
      .map(t => {
        const cabin = (t.match(/^(Saver|Main|First|Business)/i) || ['', 'Main'])[1];
        const pts = t.match(/([\d,.]+)\s*k?\s*points/i);
        const tax = t.match(/\+\s*\$([\d,.]+)/);
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

	// A route/date with no award space never renders a flight card; Alaska shows
	// a no-results message instead. Wait for whichever appears. If it was the
	// no-results message, AlaskaExtractJS finds no cards and returns a valid
	// {"flights":[]} payload, which the worker stores as a success.
	card := page.Locator(alaskaCardSel).First()
	if err := card.Or(page.GetByText(alaskaNoResultsRe)).First().WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(float64(alaskaResultsWait.Milliseconds())),
	}); err != nil {
		return fail(err)
	}
	page.WaitForTimeout(1500)

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
