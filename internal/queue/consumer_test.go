package queue

import (
	"reflect"
	"testing"
)

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
