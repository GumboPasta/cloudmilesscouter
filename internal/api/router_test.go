package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cloudmilesscouter/internal/config"
)

func testConfig() config.Config {
	return config.Config{
		CORSAllowedOrigins: []string{"http://localhost:5173"},
		RateLimitPerMinute: 3,
	}
}

func TestHealthz(t *testing.T) {
	srv := httptest.NewServer(NewRouter(testConfig(), nil, nil, nil))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	// A tiny rate-limit budget, deliberately exceeded, to prove /metrics is
	// served from ahead of the limiter.
	cfg := testConfig()
	cfg.RateLimitPerMinute = 1
	handler := NewRouter(cfg, nil, nil, nil)

	for i := 0; i < 3; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		req.RemoteAddr = "203.0.113.9:12345"
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200", i+1, rr.Code)
		}
		body, _ := io.ReadAll(rr.Body)
		if !strings.Contains(string(body), "go_goroutines") {
			t.Fatalf("request %d: body is not Prometheus exposition output", i+1)
		}
	}
}

func TestCORSPreflightAllowsConfiguredOrigin(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/healthz", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)

	NewRouter(testConfig(), nil, nil, nil).ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want http://localhost:5173", got)
	}
}

func TestCORSPreflightRejectsUnknownOrigin(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/healthz", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)

	NewRouter(testConfig(), nil, nil, nil).ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty for unknown origin", got)
	}
}

func TestRateLimitReturns429AfterBurst(t *testing.T) {
	handler := NewRouter(testConfig(), nil, nil, nil) // RateLimitPerMinute: 3

	var got429 bool
	for i := 0; i < 5; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.RemoteAddr = "203.0.113.7:12345"
		handler.ServeHTTP(rr, req)
		if rr.Code == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Fatal("expected a 429 within 5 requests over a budget of 3")
	}
}

func TestRateLimitDisabledWhenZero(t *testing.T) {
	cfg := testConfig()
	cfg.RateLimitPerMinute = 0
	handler := NewRouter(cfg, nil, nil, nil)

	for i := 0; i < 10; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.RemoteAddr = "203.0.113.7:12345"
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200 (limiter should be off)", i+1, rr.Code)
		}
	}
}
