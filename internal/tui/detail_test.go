package tui

import (
	"testing"

	"github.com/nrf110/test-term/internal/event"
)

func TestFormatLoc(t *testing.T) {
	cases := []struct {
		loc  event.Location
		want string
	}{
		{event.Location{File: "a.go", Line: 42, Col: 7}, "a.go:42:7"},
		{event.Location{File: "a.go", Line: 42}, "a.go:42"},
		{event.Location{File: "a.go"}, "a.go"},
		{event.Location{}, ""},
	}
	for _, c := range cases {
		if got := formatLoc(c.loc); got != c.want {
			t.Errorf("formatLoc(%+v) = %q, want %q", c.loc, got, c.want)
		}
	}
}

func TestDurationFormat(t *testing.T) {
	cases := map[int64]string{0: "0ms", 12: "12ms", 999: "999ms", 1000: "1.00s", 1800: "1.80s"}
	for ms, want := range cases {
		if got := duration(ms); got != want {
			t.Errorf("duration(%d) = %q, want %q", ms, got, want)
		}
	}
}
