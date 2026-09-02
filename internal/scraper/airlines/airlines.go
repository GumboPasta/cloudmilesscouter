package airlines

import (
	"cloudmilesscouter/internal/config"
	"cloudmilesscouter/internal/scraper"
)

// ScrapeFunc runs one airline's award search and returns its raw response body.
// cfg carries the per-airline knobs (profile dir, credentials, headless) each
// scraper needs; params is the shared route/date.
type ScrapeFunc func(cfg config.Config, params scraper.SearchParams) ([]byte, error)

// Scrapers maps an airline ID (the ScrapeJob.Airline value the producer sets)
// to its scraper. Adding an airline is a new file in this package plus an entry
// here — mirrors etl.parsersByAirline.
var Scrapers = map[string]ScrapeFunc{
	"united": func(cfg config.Config, params scraper.SearchParams) ([]byte, error) {
		return Scrape(cfg.UnitedProfileDir, cfg.Headless, params, cfg.UnitedPassword)
	},
	"american": func(cfg config.Config, params scraper.SearchParams) ([]byte, error) {
		return ScrapeAmerican(cfg.AmericanProfileDir, cfg.Headless, params)
	},
	"delta": func(cfg config.Config, params scraper.SearchParams) ([]byte, error) {
		return ScrapeDelta(cfg.DeltaProfileDir, cfg.Headless, params)
	},
	"alaska": func(cfg config.Config, params scraper.SearchParams) ([]byte, error) {
		return ScrapeAlaska(cfg.AlaskaProfileDir, cfg.Headless, params)
	},
}

// HasResultsFor dispatches to the airline-specific "did this scrape return any
// flights" check. Every airline returns HTTP 200 with an empty result set for a
// route/date with no award space — valid data, not a failure — so callers log
// it rather than error. An unknown airline reports true (nothing to warn about).
func HasResultsFor(airline string, body []byte) (bool, error) {
	switch airline {
	case "united":
		return HasResults(body)
	case "american":
		return HasResultsAmerican(body)
	case "delta":
		return HasResultsDelta(body)
	case "alaska":
		return HasResultsAlaska(body)
	default:
		return true, nil
	}
}
