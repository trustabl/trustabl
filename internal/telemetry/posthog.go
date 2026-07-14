package telemetry

import (
	"io"
	"log"
	"time"

	"github.com/posthog/posthog-go"
)

// postHogSink sends events to PostHog cloud. Track enqueues asynchronously
// (posthog-go batches internally); Flush blocks until all events are sent.
type postHogSink struct {
	client posthog.Client
}

func newPostHogSink(apiKey string) Sink {
	client, err := posthog.NewWithConfig(apiKey, posthog.Config{
		Endpoint: "https://us.i.posthog.com",
		// Use a short flush interval so events aren't held too long.
		Interval: 500 * time.Millisecond,
		// Suppress PostHog's own verbose logging.
		Verbose: false,
		Logger:  posthog.StdLogger(log.New(io.Discard, "", 0), false),
	})
	if err != nil {
		// Misconfigured key — fall back to null sink so the scan still runs.
		return NewNullSink()
	}
	return &postHogSink{client: client}
}

func (s *postHogSink) Track(event string, props map[string]any) {
	ph := posthog.NewProperties()
	for k, v := range props {
		ph.Set(k, v)
	}
	_ = s.client.Enqueue(posthog.Capture{
		DistinctId: stringProp(props, "anonymous_id"),
		Event:      event,
		Properties: ph,
	})
}

func (s *postHogSink) Flush() {
	_ = s.client.Close()
}

func stringProp(props map[string]any, key string) string {
	if v, ok := props[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return "unknown"
}
