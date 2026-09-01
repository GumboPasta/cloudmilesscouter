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
}
