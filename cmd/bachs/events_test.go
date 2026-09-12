package main

import (
	"testing"

	"github.com/bachsdev/bachs-cli/internal/api"
)

func strp(s string) *string { return &s }
func intp(i int) *int       { return &i }

// The mark separates three outcomes that are easy to confuse, and confusing
// them is what sent one debugging session down the wrong path entirely:
// nothing was listening, delivery was tried and failed, delivery worked.
func TestDeliveryMark(t *testing.T) {
	cases := []struct {
		name  string
		event api.Event
		want  string
	}{
		{"nothing listening", api.Event{Attempts: 0}, "-"},
		{"all failed", api.Event{Attempts: 2, Failed: 2}, "✗"},
		{"partly failed", api.Event{Attempts: 3, Success: 1, Failed: 2}, "!"},
		{"delivered", api.Event{Attempts: 1, Success: 1}, "✓"},
	}
	for _, c := range cases {
		if got, _ := deliveryMark(c.event); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// An event with no attempts must say so in words. "0 attempts" reads as a
// counter someone can ignore; "no destination was listening" is the answer.
func TestUndeliveredSummaryExplainsItself(t *testing.T) {
	got := deliverySummary(api.Event{Attempts: 0})
	if !contains(got, "no destination was listening") {
		t.Errorf("got %q, want it to explain that nothing was listening", got)
	}
}

func TestSummaryReportsTheLastHTTPStatus(t *testing.T) {
	got := deliverySummary(api.Event{
		Attempts:              2,
		Failed:                2,
		LastAttemptHTTPStatus: intp(500),
	})
	if !contains(got, "last HTTP 500") {
		t.Errorf("got %q, want the last HTTP status", got)
	}
}

// A connection error has no HTTP status, so the error text is all the reader
// has. Truncate it, but do not drop it.
func TestSummaryFallsBackToTheErrorText(t *testing.T) {
	got := deliverySummary(api.Event{
		Attempts:         1,
		Failed:           1,
		LastAttemptError: strp("dial tcp 127.0.0.1:3000: connect: connection refused"),
	})
	if !contains(got, "dial tcp") {
		t.Errorf("got %q, want the connection error", got)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	}()
}
