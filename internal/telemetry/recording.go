package telemetry

// TrackedEvent is one captured Track call.
type TrackedEvent struct {
	Name  string
	Props map[string]any
}

// RecordingSink captures Track calls for assertions in tests.
type RecordingSink struct {
	Events []TrackedEvent
}

func NewRecordingSink() *RecordingSink { return &RecordingSink{} }

func (r *RecordingSink) Track(event string, props map[string]any) {
	r.Events = append(r.Events, TrackedEvent{Name: event, Props: props})
}

func (r *RecordingSink) Flush() {}
