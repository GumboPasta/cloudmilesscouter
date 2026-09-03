package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cloudmilesscouter/internal/queue"
)

// fakeDispatcher records the jobs handleScrape enqueues and can be made to fail.
type fakeDispatcher struct {
	jobs   []queue.ScrapeJob
	failOn string // airline whose Enqueue returns an error; "" never fails
}

func (f *fakeDispatcher) Enqueue(_ context.Context, job queue.ScrapeJob) error {
	if job.Airline == f.failOn {
		return errors.New("kafka down")
	}
	f.jobs = append(f.jobs, job)
	return nil
}

func postScrape(t *testing.T, d scrapeDispatcher, body string) *httptest.ResponseRecorder {
	t.Helper()
	srv := &server{dispatcher: d}
	req := httptest.NewRequest(http.MethodPost, "/api/scrape", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handleScrape(rec, req)
	return rec
}

func TestHandleScrapeDefaultAirlines(t *testing.T) {
	d := &fakeDispatcher{}
	rec := postScrape(t, d, `{"origin":"bos","destination":"sfo","date":"2026-12-20"}`)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body %s", rec.Code, rec.Body)
	}

	var resp scrapeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Dispatched) != len(defaultScrapeAirlines) {
		t.Fatalf("dispatched %v, want %v", resp.Dispatched, defaultScrapeAirlines)
	}
	if resp.Origin != "BOS" || resp.Destination != "SFO" || resp.Date != "2026-12-20" {
		t.Fatalf("echoed search wrong: %+v", resp)
	}
	if len(d.jobs) != len(defaultScrapeAirlines) {
		t.Fatalf("enqueued %d jobs, want %d", len(d.jobs), len(defaultScrapeAirlines))
	}
	for _, job := range d.jobs {
		if job.Origin != "BOS" || job.Destination != "SFO" || job.Date != "2026-12-20" {
			t.Fatalf("job has wrong search: %+v", job)
		}
	}
}

func TestHandleScrapeAirlineOverride(t *testing.T) {
	d := &fakeDispatcher{}
	rec := postScrape(t, d, `{"origin":"JFK","destination":"LAX","date":"2026-06-01","airlines":["Delta"," delta ","alaska"]}`)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body %s", rec.Code, rec.Body)
	}
	got := []string{}
	for _, job := range d.jobs {
		got = append(got, job.Airline)
	}
	if strings.Join(got, ",") != "delta,alaska" {
		t.Fatalf("airlines = %v, want [delta alaska] (lower-cased, de-duped)", got)
	}
}

func TestHandleScrapeBadRequests(t *testing.T) {
	cases := map[string]string{
		"not JSON":       `not json`,
		"unknown field":  `{"origin":"BOS","destination":"SFO","date":"2026-12-20","foo":1}`,
		"missing origin": `{"destination":"SFO","date":"2026-12-20"}`,
		"short origin":   `{"origin":"BO","destination":"SFO","date":"2026-12-20"}`,
		"missing date":   `{"origin":"BOS","destination":"SFO"}`,
		"bad date":       `{"origin":"BOS","destination":"SFO","date":"12/20/2026"}`,
		"blank airlines": `{"origin":"BOS","destination":"SFO","date":"2026-12-20","airlines":["  ",""]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			d := &fakeDispatcher{}
			rec := postScrape(t, d, body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body %s", rec.Code, rec.Body)
			}
			if len(d.jobs) != 0 {
				t.Fatalf("enqueued %d jobs on a bad request, want 0", len(d.jobs))
			}
		})
	}
}

func TestHandleScrapeDispatchFailure(t *testing.T) {
	d := &fakeDispatcher{failOn: "delta"}
	rec := postScrape(t, d, `{"origin":"BOS","destination":"SFO","date":"2026-12-20"}`)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body %s", rec.Code, rec.Body)
	}
}
