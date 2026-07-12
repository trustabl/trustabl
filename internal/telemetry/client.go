// internal/telemetry/client.go
package telemetry

import (
	"os"
)

// Client resolves opt-in mode, manages the anonymous ID, and forwards Track
// calls to the underlying Sink. Safe for concurrent use.
type Client struct {
	sink         Sink
	anonymousID  string
	mode         string
	ciProvider   string
	isNewInstall bool
	version      string
}

// New constructs a Client for production use. apiKey is the PostHog project
// key (empty = NullSink). configPath is the path to telemetry.json.
// stderr is reserved for the first-run prompt (Task 3); pass nil to suppress.
func New(apiKey, version, configPath string, stderr *os.File) *Client {
	envVal := os.Getenv("TRUSTABL_TELEMETRY")

	cfg, existed, err := LoadConfig(configPath)
	if err != nil {
		id, _ := newUUID()
		cfg = Config{Mode: "", AnonymousID: id}
		existed = false
	}

	mode := resolveMode(cfg.Mode, envVal)

	isCI := os.Getenv("CI") != "" || DetectCIProvider() != ""
	if isCI && mode == "" {
		mode = "disabled"
	}
	if mode == "" {
		// No config, no env var, not CI: default to disabled.
		// Task 3 replaces this with an interactive prompt on TTY.
		mode = "disabled"
	}

	anonymousID := cfg.AnonymousID
	if isCI && !existed {
		id, _ := newUUID()
		anonymousID = id
	}

	var sink Sink
	if mode != "disabled" && apiKey != "" {
		sink = newPostHogSink(apiKey)
	} else {
		sink = NewNullSink()
	}

	return &Client{
		sink:         sink,
		anonymousID:  anonymousID,
		mode:         mode,
		ciProvider:   DetectCIProvider(),
		isNewInstall: !existed,
		version:      version,
	}
}

// NewWithSink constructs a Client with an explicit sink (for tests).
func NewWithSink(sink Sink, version, configPath string) *Client {
	envVal := os.Getenv("TRUSTABL_TELEMETRY")
	cfg, existed, err := LoadConfig(configPath)
	if err != nil {
		id, _ := newUUID()
		cfg = Config{Mode: "", AnonymousID: id}
		existed = false
	}
	mode := resolveMode(cfg.Mode, envVal)
	if mode == "" {
		mode = "disabled"
	}
	if mode == "disabled" {
		sink = NewNullSink()
	}
	return &Client{
		sink:         sink,
		anonymousID:  cfg.AnonymousID,
		mode:         mode,
		ciProvider:   DetectCIProvider(),
		isNewInstall: !existed,
		version:      version,
	}
}

// resolveMode applies env var override on top of the config mode.
// Returns "" when neither source has set a mode.
func resolveMode(cfgMode, envVal string) string {
	switch envVal {
	case "0", "disabled":
		return "disabled"
	case "1", "full":
		return "full"
	case "minimal":
		return "minimal"
	}
	return cfgMode
}

// Track sends an event to the sink. In "minimal" mode only scan.completed
// and scan.failed fire, with exactly 5 properties. In "full" mode all events
// and properties are forwarded. No-op when disabled.
func (c *Client) Track(event string, props map[string]any) {
	if c.mode == "disabled" {
		return
	}

	if c.mode == "minimal" {
		if event != "scan.completed" && event != "scan.failed" {
			return
		}
		minimal := map[string]any{
			"anonymous_id":  c.anonymousID,
			"cli_version":   c.version,
			"ci_provider":   c.ciProvider,
			"is_new_install": c.isNewInstall,
		}
		if v, ok := props["exit_code"]; ok {
			minimal["exit_code"] = v
		}
		c.sink.Track(event, minimal)
		return
	}

	merged := make(map[string]any, len(props)+2)
	for k, v := range props {
		merged[k] = v
	}
	merged["anonymous_id"] = c.anonymousID
	merged["cli_version"] = c.version
	c.sink.Track(event, merged)
}

// Flush blocks until all queued events are delivered.
func (c *Client) Flush() { c.sink.Flush() }

// IsEnabled reports whether telemetry is active (mode is not "disabled").
func (c *Client) IsEnabled() bool { return c.mode != "disabled" }

// Mode reports the current telemetry mode: "disabled", "minimal", or "full".
func (c *Client) Mode() string { return c.mode }

// IsNewInstall reports whether the config file did not exist before this run.
func (c *Client) IsNewInstall() bool { return c.isNewInstall }
