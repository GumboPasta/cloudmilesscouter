package queue

import (
	"reflect"
	"testing"
)

func TestConsumerLagZeroBeforeFetch(t *testing.T) {
	c := NewConsumer("localhost:9092", "test-group")
	defer c.Close()

	// kafka-go only computes lag on a fetch, so a fresh reader reports 0 without
	// needing a live broker. The point of the test is that the accessor is safe
	// to call from the worker's gauge ticker at any time.
	if got := c.Lag(); got != 0 {
		t.Fatalf("Lag() = %d, want 0 before any fetch", got)
	}
}

func TestSplitBrokers(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"localhost:9092", []string{"localhost:9092"}},
		{"h1:9092,h2:9092", []string{"h1:9092", "h2:9092"}},
		{" h1:9092 , h2:9092 ", []string{"h1:9092", "h2:9092"}},
		{"h1:9092,,h2:9092,", []string{"h1:9092", "h2:9092"}},
		{"", nil},
		{"  ", nil},
	}
	for _, c := range cases {
		if got := splitBrokers(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitBrokers(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}
}
