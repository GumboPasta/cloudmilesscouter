package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSearchValidation covers the request-validation branch of handleSearch:
// every case here is rejected before the DB is touched, so a nil *sql.DB is safe.
func TestSearchValidation(t *testing.T) {
	cfg := testConfig()
	cfg.RateLimitPerMinute = 0 // these cases all hit the same IP; don't let the limiter mask a 400
	handler := NewRouter(cfg, nil, nil, nil)

	cases := []struct {
		name  string
		query string
	}{
		{"missing origin", "destination=LAX&date=2026-06-01"},
		{"missing destination", "origin=JFK&date=2026-06-01"},
		{"missing date", "origin=JFK&destination=LAX"},
		{"origin not 3 letters", "origin=JF&destination=LAX&date=2026-06-01"},
		{"origin has digits", "origin=JF1&destination=LAX&date=2026-06-01"},
		{"bad date format", "origin=JFK&destination=LAX&date=06-01-2026"},
		{"unknown cabin", "origin=JFK&destination=LAX&date=2026-06-01&cabin=economy_plus"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/search?"+tc.query, nil)
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rr.Code)
			}
			var body map[string]string
			if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["error"] == "" {
				t.Fatalf("expected an error message, got %v", body)
			}
		})
	}
}
