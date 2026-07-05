// internal/telemetry/client.go
package telemetry

import (
	"os"

	"github.com/mattn/go-isatty"
)

// Client resolves opt-out, manages the anonymous ID, and forwards Track calls
// to the underlying Sink. Safe for concurrent use.
type Client struct {
	sink         Sink
	anonymousID  string
	enabled      bool
	isNewInstall bool
	version      string
}

// New constructs a Client for production use. apiKey is the PostHog project
// key (empty = NullSink). configPath is the path to telemetry.json.
// stderr is used for the first-run notice; pass nil to suppress it.
func New(apiKey, version, configPath string, stderr *os.File) *Client {
	envVal := os.Getenv("TRUSTABL_TELEMETRY")

	cfg, existed, err := LoadConfig(configPath)
	if err != nil {
		// Unreadable config → treat as missing (enabled, fresh ID)
		id, _ := newUUID()
		cfg = Config{Enabled: true, AnonymousID: id}
		existed = false
	}

	// Opt-out precedence: env var > config > default.
	// Spec note: config is read before the TRUSTABL_TELEMETRY=0 fast-path so
	// that we always have an AnonymousID available (for a stable per-machine
	// identity even when telemetry is disabled). The enabled flag from config
	// is overridden below, so no telemetry is sent when envVal=="0".
	enabled := cfg.Enabled
	switch envVal {
	case "0":
		enabled = false
	case "1":
		enabled = true
	}

	anonymousID := cfg.AnonymousID
	// CI environments get an ephemeral ID when no config file is present.
	isCI := os.Getenv("CI") != "" || DetectCIProvider() != ""
	if isCI && !existed {
		id, _ := newUUID()
		anonymousID = id
	}

	// Persist config on first run so subsequent runs are stable.
	// CI environments use ephemeral IDs (not stored) — skip the write there.
	if !existed && !isCI && envVal != "0" {
		_ = SaveConfig(configPath, Config{Enabled: true, AnonymousID: cfg.AnonymousID})
	}

	// Print first-run notice on TTY only, once (when config didn't exist).
	if !existed && enabled && stderr != nil && isatty.IsTerminal(stderr.Fd()) {
		printFirstRunNotice(stderr)
	}

	var sink Sink
	if enabled && apiKey != "" {
		sink = newPostHogSink(apiKey)
	} else {
		sink = NewNullSink()
	}

	return &Client{
		sink:         sink,
		anonymousID:  anonymousID,
		enabled:      enabled,
		isNewInstall: !existed,
		version:      version,
	}
}

// NewWithSink constructs a Client with an explicit sink (for tests).
func NewWithSink(sink Sink, version, configPath string) *Client {
	envVal := os.Getenv("TRUSTABL_TELEMETRY")
	cfg, existed, err := LoadConfig(configPath)
	if err != nil {
		// Unreadable/corrupt config → default to enabled so the contract
		// "telemetry is on unless opted out" is upheld.
		id, _ := newUUID()
		cfg = Config{Enabled: true, AnonymousID: id}
		existed = false
	}
	enabled := cfg.Enabled
	switch envVal {
	case "0":
		enabled = false
	case "1":
		enabled = true
	}
	if !enabled {
		sink = NewNullSink()
	}
	return &Client{
		sink:         sink,
		anonymousID:  cfg.AnonymousID,
		enabled:      enabled,
		isNewInstall: !existed,
		version:      version,
	}
}

// Track sends an event to the sink. Props are merged with base properties
// (anonymous_id, cli_version). No-op when the client is disabled.
func (c *Client) Track(event string, props map[string]any) {
	if !c.enabled {
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

// Flush blocks until all queued events are delivered (up to the sink's
// internal timeout). Call once at process exit.
func (c *Client) Flush() { c.sink.Flush() }

// IsEnabled reports whether telemetry is active.
func (c *Client) IsEnabled() bool { return c.enabled }

// IsNewInstall reports whether the config file did not exist before this run.
func (c *Client) IsNewInstall() bool { return c.isNewInstall }

func printFirstRunNotice(w *os.File) {
	const notice = "\nTrustabl collects anonymous usage data to help improve the product.\n" +
		"No source code, file paths, repo names, or finding details are ever sent.\n" +
		"Run `trustabl telemetry off` or set TRUSTABL_TELEMETRY=0 to disable.\n" +
		"Learn more: https://trustabl.dev/telemetry\n\n"
	_, _ = w.WriteString(notice)
}
