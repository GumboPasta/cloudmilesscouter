package metrics

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandlerExposesInstruments checks that importing the package registers the
// collectors and that Handler serves them in the Prometheus text format. It
// touches one metric first so the family is present in the output.
func TestHandlerExposesInstruments(t *testing.T) {
	// A *Vec emits nothing until it has a child series, so touch each one.
	HTTPRequestsTotal.WithLabelValues("GET", "/api/search", "200").Inc()
	HTTPRequestDuration.WithLabelValues("GET", "/api/search").Observe(0.01)
	SearchCacheRequests.WithLabelValues("hit").Inc()
	ScrapeAttemptsTotal.WithLabelValues("united").Inc()
	ScrapeFailuresTotal.WithLabelValues("united", "timeout").Inc()
	KafkaConsumerLag.WithLabelValues("scrape.jobs").Set(3)
	ScrapeCircuitState.WithLabelValues("united").Set(1)
	DLQMessagesTotal.WithLabelValues("united", "timeout").Inc()
	ETLParseFailuresTotal.WithLabelValues("delta").Inc()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	Handler().ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body, _ := io.ReadAll(rr.Body)
	got := string(body)

	for _, name := range []string{
		"http_requests_total",
		"http_request_duration_seconds",
		"search_cache_requests_total",
		"scrape_attempts_total",
		"scrape_failures_total",
		"kafka_consumer_lag",
		"scrape_circuit_state",
		"dlq_messages_total",
		"etl_parse_failures_total",
	} {
		if !strings.Contains(got, name) {
			t.Errorf("exposition output missing %q", name)
		}
	}
}
