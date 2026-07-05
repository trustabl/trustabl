package telemetry

func newPostHogSink(_ string) Sink { return NewNullSink() }
