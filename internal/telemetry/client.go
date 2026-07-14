// internal/telemetry/client.go
package telemetry

import (
	"bufio"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
)

// Client resolves opt-in mode, manages the anonymous ID, and forwards Track
// calls to the underlying Sink. Safe for concurrent use.
type Client struct {
	sink         Sink
	crashSink    Sink // crash.reported transport; live whenever a key exists, independent of mode
	anonymousID  string
	mode         string
	ciProvider   string
	isNewInstall bool
	version      string
}

// New constructs a Client for production use. apiKey is the PostHog project
// key (empty = NullSink). configPath is the path to telemetry.json.
// stderr is reserved for the first-run prompt (Task 3); pass nil to suppress.
// stdin is used for the first-run prompt; pass nil to suppress interactive mode.
func New(apiKey, version, configPath string, stderr *os.File, stdin io.Reader) *Client {
	envVal := os.Getenv("TRUSTABL_TELEMETRY")

	cfg, existed, err := LoadConfig(configPath)
	if err != nil {
		id, _ := newUUID()
		cfg = Config{Mode: "", AnonymousID: id}
		existed = false
	}

	mode := resolveMode(cfg.Mode, envVal)

	ciProvider := DetectCIProvider()
	isCI := os.Getenv("CI") != "" || ciProvider != ""
	if isCI && mode == "" {
		mode = "disabled"
	}

	// First-run prompt: mode unset, interactive TTY, not CI, stdin provided.
	if mode == "" && !isCI && stderr != nil && isatty.IsTerminal(stderr.Fd()) && stdin != nil {
		mode = PromptMode(stderr, stdin)
		_ = SaveConfig(configPath, Config{Mode: mode, AnonymousID: cfg.AnonymousID})
	}

	// Non-TTY or non-interactive: default to disabled without saving.
	if mode == "" {
		mode = "disabled"
	}

	anonymousID := cfg.AnonymousID
	if isCI && !existed {
		id, _ := newUUID()
		anonymousID = id
	}

	// The crash transport is live whenever a PostHog key is built into the binary,
	// regardless of telemetry mode — crash reporting is a separate consent from
	// usage telemetry (the per-crash prompt is the consent). The usage-telemetry
	// sink shares that same PostHog client when telemetry is enabled, and is a
	// NullSink otherwise.
	var telSink, crashSink Sink = NewNullSink(), NewNullSink()
	if apiKey != "" {
		ph := newPostHogSink(apiKey)
		crashSink = ph
		if mode != "disabled" {
			telSink = ph
		}
	}

	return &Client{
		sink:         telSink,
		crashSink:    crashSink,
		anonymousID:  anonymousID,
		mode:         mode,
		ciProvider:   ciProvider,
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
	// The provided sink is the crash transport regardless of mode, so tests can
	// verify crash sends even when telemetry is disabled. The usage-telemetry
	// sink is nulled when disabled.
	telSink := sink
	if mode == "disabled" {
		telSink = NewNullSink()
	}
	return &Client{
		sink:         telSink,
		crashSink:    sink,
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
			"anonymous_id":   c.anonymousID,
			"cli_version":    c.version,
			"ci_provider":    c.ciProvider,
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

// TrackCrash sends the crash.reported event over the dedicated crash transport.
// It is fully independent of the usage-telemetry level: it fires the same way in
// full, minimal, AND disabled modes, because the per-crash prompt is its own
// explicit consent, separate from telemetry. It only no-ops when no PostHog key
// is built into the binary (nowhere to send).
func (c *Client) TrackCrash(props map[string]any) {
	merged := make(map[string]any, len(props)+2)
	for k, v := range props {
		merged[k] = v
	}
	merged["anonymous_id"] = c.anonymousID
	merged["cli_version"] = c.version
	c.crashSink.Track("crash.reported", merged)
}

// Flush blocks until all queued events are delivered. Both the usage-telemetry
// sink and the crash transport are flushed; when telemetry is enabled they share
// one PostHog client and the second flush is a harmless no-op.
func (c *Client) Flush() {
	c.sink.Flush()
	if c.crashSink != c.sink {
		c.crashSink.Flush()
	}
}

// IsEnabled reports whether telemetry is active (mode is not "disabled").
func (c *Client) IsEnabled() bool { return c.mode != "disabled" }

// Mode reports the current telemetry mode: "disabled", "minimal", or "full".
func (c *Client) Mode() string { return c.mode }

// IsNewInstall reports whether the config file did not exist before this run.
func (c *Client) IsNewInstall() bool { return c.isNewInstall }

// PromptMode writes the telemetry choice prompt to w, reads a response from r,
// and returns the chosen mode. Re-prompts once on invalid input; defaults to
// "disabled" on empty input, second invalid input, or EOF.
func PromptMode(w io.Writer, r io.Reader) string {
	const intro = "\nTrustabl collects anonymous data to help improve the product.\n" +
		"No source code, file paths, repo names, or finding details are ever sent.\n" +
		"Learn more: https://trustabl.ai/telemetry\n\n" +
		"Choose a telemetry level:\n" +
		"  1. Minimal  - Version and outcome\n" +
		"  2. Full     - Usage stats\n" +
		"  3. Disabled - No data\n\n" +
		"Enter 1, 2, or 3 [default: 3]: "
	_, _ = io.WriteString(w, intro)

	scanner := bufio.NewScanner(r)
	for attempt := 0; attempt < 2; attempt++ {
		if !scanner.Scan() {
			return "disabled"
		}
		switch strings.TrimSpace(scanner.Text()) {
		case "1":
			return "minimal"
		case "2":
			return "full"
		case "", "3":
			return "disabled"
		}
		if attempt == 0 {
			_, _ = io.WriteString(w, "Please enter 1, 2, or 3 [default: 3]: ")
		}
	}
	return "disabled"
}
