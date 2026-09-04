package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// newTestLogger mirrors Setup but writes to buf so a test can inspect the
// output. Setup itself is hard-wired to os.Stderr.
func newTestLogger(buf *bytes.Buffer, service, level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}
	var h slog.Handler
	if format == "text" {
		h = slog.NewTextHandler(buf, opts)
	} else {
		h = slog.NewJSONHandler(buf, opts)
	}
	return slog.New(h.WithAttrs([]slog.Attr{slog.String("service", service)}))
}

func TestJSONFormatCarriesServiceAndLevel(t *testing.T) {
	var buf bytes.Buffer
	newTestLogger(&buf, "worker", "info", "json").Info("job done", "airline", "delta")

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if line["service"] != "worker" {
		t.Errorf("service = %v, want worker", line["service"])
	}
	if line["level"] != "INFO" {
		t.Errorf("level = %v, want INFO", line["level"])
	}
	if line["msg"] != "job done" || line["airline"] != "delta" {
		t.Errorf("unexpected message fields: %v", line)
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf, "api", "info", "json")

	log.Debug("noisy")
	if buf.Len() != 0 {
		t.Errorf("debug line emitted at info level: %s", buf.String())
	}

	log.Warn("kept")
	if !strings.Contains(buf.String(), "kept") {
		t.Errorf("warn line dropped at info level: %s", buf.String())
	}
}

func TestParseLevelUnknownFallsBackToInfo(t *testing.T) {
	if got := parseLevel("bogus"); got != slog.LevelInfo {
		t.Errorf("parseLevel(bogus) = %v, want INFO", got)
	}
	if got := parseLevel("debug"); got != slog.LevelDebug {
		t.Errorf("parseLevel(debug) = %v, want DEBUG", got)
	}
}

func TestTextFormat(t *testing.T) {
	var buf bytes.Buffer
	newTestLogger(&buf, "etl", "info", "text").Info("etl run complete")

	out := buf.String()
	if !strings.Contains(out, "service=etl") || !strings.Contains(out, `msg="etl run complete"`) {
		t.Errorf("text output missing expected key=value pairs: %s", out)
	}
}
