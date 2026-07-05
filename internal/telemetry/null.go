package telemetry

// NullSink discards all events. Used for opted-out users and in tests.
type NullSink struct{}

func NewNullSink() *NullSink        { return &NullSink{} }
func (NullSink) Track(string, map[string]any) {}
func (NullSink) Flush()                        {}
