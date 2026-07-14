package telemetry

// Sink receives telemetry events. Implementations must be safe for concurrent
// use. Track must return immediately; delivery may happen asynchronously.
type Sink interface {
	Track(event string, props map[string]any)
	Flush()
}
