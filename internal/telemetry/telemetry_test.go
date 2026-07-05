package telemetry_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/telemetry"
)

func TestNullSink_noop(t *testing.T) {
	s := telemetry.NewNullSink()
	s.Track("any.event", map[string]any{"key": "value"}) // must not panic
	s.Flush()                                             // must not panic
}

func TestRecordingSink_capturesEvents(t *testing.T) {
	s := telemetry.NewRecordingSink()
	s.Track("scan.started", map[string]any{"os": "darwin"})
	s.Track("scan.completed", map[string]any{"exit_code": 0})

	if len(s.Events) != 2 {
		t.Fatalf("want 2 events, got %d", len(s.Events))
	}
	if s.Events[0].Name != "scan.started" {
		t.Errorf("want scan.started, got %s", s.Events[0].Name)
	}
	if s.Events[1].Props["exit_code"] != 0 {
		t.Errorf("want exit_code=0, got %v", s.Events[1].Props["exit_code"])
	}
}
