package queue

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDLQTopic(t *testing.T) {
	if DLQTopic != "scrape.jobs.dlq" {
		t.Fatalf("DLQTopic = %q, want %q", DLQTopic, "scrape.jobs.dlq")
	}
	if DLQTopic == Topic {
		t.Fatal("DLQTopic must differ from Topic")
	}
}

func TestDeadLetterJobRoundTrip(t *testing.T) {
	failedAt := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	in := DeadLetterJob{
		Job:      ScrapeJob{Airline: "united", Origin: "BOS", Destination: "SFO", Date: "2026-12-20", Attempt: 3},
		Reason:   "timeout",
		Attempts: 3,
		Error:    "navigation timed out",
		FailedAt: failedAt,
	}

	payload, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out DeadLetterJob
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Job != in.Job {
		t.Errorf("job = %+v, want %+v", out.Job, in.Job)
	}
	if out.Reason != in.Reason || out.Attempts != in.Attempts || out.Error != in.Error {
		t.Errorf("metadata = %+v, want reason/attempts/error %q/%d/%q", out, in.Reason, in.Attempts, in.Error)
	}
	if !out.FailedAt.Equal(in.FailedAt) {
		t.Errorf("failed_at = %s, want %s", out.FailedAt, in.FailedAt)
	}
}
