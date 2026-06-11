package event

import (
	"encoding/json"
	"testing"
)

func TestEventJSONRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		ev   Event
	}{
		{"run_started", RunStarted("run-1", "all")},
		{
			"node_discovered",
			NodeDiscovered("go:./auth::TestLogin", "go:./auth", "TestLogin", KindTest,
				&Location{File: "auth/login_test.go", Line: 12, Col: 2}),
		},
		{"node_started", NodeStarted("go:./auth::TestLogin")},
		{
			"node_finished_pass",
			NodeFinished("go:./auth::TestLogin", StatusPass, 12, nil),
		},
		{
			"node_finished_fail",
			NodeFinished("go:./auth::TestLogin", StatusFail, 8, &Failure{
				Message: "expected 200 to be 401",
				Diff:    "- 401\n+ 200",
				Frames:  []Frame{{Function: "TestLogin", File: "auth/login_test.go", Line: 42, Col: 7}},
			}),
		},
		{"output", Output("go:./auth::TestLogin", StreamStderr, "boom\n")},
		{
			"run_finished",
			RunFinished("run-1", Summary{Total: 10, Passed: 8, Failed: 1, Skipped: 1, DurationMs: 1800}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.ev)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got Event
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			// Re-marshal and compare bytes: a stable round-trip proves no field
			// was dropped or mistyped.
			b2, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("re-marshal: %v", err)
			}
			if string(b) != string(b2) {
				t.Fatalf("round-trip mismatch:\n first: %s\nsecond: %s", b, b2)
			}
		})
	}
}

func TestEventTypeIsDiscriminated(t *testing.T) {
	b, _ := json.Marshal(NodeStarted("n1"))
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["type"] != string(TypeNodeStarted) {
		t.Fatalf("type field = %v, want %q", m["type"], TypeNodeStarted)
	}
	// Zero-valued optional fields must be omitted from the wire form.
	if _, ok := m["status"]; ok {
		t.Fatalf("empty status should be omitted, got payload %s", b)
	}
}

func TestStatusHelpers(t *testing.T) {
	terminal := map[Status]bool{
		StatusPending: false, StatusRunning: false,
		StatusPass: true, StatusFail: true, StatusSkip: true, StatusError: true,
	}
	for s, want := range terminal {
		if s.Terminal() != want {
			t.Errorf("%s.Terminal() = %v, want %v", s, s.Terminal(), want)
		}
	}
	failing := map[Status]bool{
		StatusPass: false, StatusSkip: false, StatusPending: false, StatusRunning: false,
		StatusFail: true, StatusError: true,
	}
	for s, want := range failing {
		if s.Failing() != want {
			t.Errorf("%s.Failing() = %v, want %v", s, s.Failing(), want)
		}
	}
}
