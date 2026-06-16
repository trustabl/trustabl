package rulesign

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrInvalidChannel means a channel name is unsafe to use as a path component.
// Channel names are trusted tokens (production, staging, legacy-*), so anything
// outside [a-z0-9._-] — notably a path separator or "." traversal — is refused
// rather than allowed to escape the state directory.
var ErrInvalidChannel = errors.New("rulesign: invalid channel name")

// channelState is the per-channel record persisted in the cache. Version is the
// anti-rollback floor VerifyStatement enforces; Digest is the bundle the floor
// points at (the offline pointer); Expires is the last-verified statement's
// freshness deadline, used to warn when an offline scan is serving stale rules.
type channelState struct {
	Version int64  `json:"version"`
	Digest  string `json:"digest,omitempty"`
	Expires string `json:"expires,omitempty"` // RFC3339, for offline staleness
}

// validChannelName reports whether name is a safe single path component.
func validChannelName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		ok := c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

func channelStatePath(stateDir, channel string) (string, error) {
	if !validChannelName(channel) {
		return "", fmt.Errorf("%w: %q", ErrInvalidChannel, channel)
	}
	return filepath.Join(stateDir, "channels", channel+".json"), nil
}

// readState loads the persisted record for channel, reporting found=false when
// none exists yet. A corrupt record is an error, not a silent empty state — a
// wiped floor is exactly what a rollback attack wants.
func readState(stateDir, channel string) (st channelState, found bool, err error) {
	path, err := channelStatePath(stateDir, channel)
	if err != nil {
		return channelState{}, false, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return channelState{}, false, nil
	}
	if err != nil {
		return channelState{}, false, fmt.Errorf("rulesign: read channel state: %w", err)
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return channelState{}, false, fmt.Errorf("rulesign: corrupt channel state %s: %w", path, err)
	}
	return st, true, nil
}

// ReadLastSeenVersion returns the highest statement version previously recorded
// for channel under stateDir, or 0 if none has been recorded yet (a fresh
// machine has no floor and accepts any positive version).
func ReadLastSeenVersion(stateDir, channel string) (int64, error) {
	st, _, err := readState(stateDir, channel)
	return st.Version, err
}

// ChannelPointer reports the cached bundle digest, its version, and the
// last-verified statement's expiry for channel — the inputs an offline resolve
// needs to serve the last good bundle and decide whether to warn that it is
// stale. found is false when nothing has been recorded yet.
func ChannelPointer(stateDir, channel string) (digest string, version int64, expires string, found bool, err error) {
	st, found, err := readState(stateDir, channel)
	if err != nil || !found {
		return "", 0, "", found, err
	}
	return st.Digest, st.Version, st.Expires, true, nil
}

// RecordStatement persists s as the new floor for its channel under stateDir,
// but only if its version advances the existing floor — so a re-resolve of the
// current pointer is idempotent and a regression can never lower the floor on
// disk. It returns the floor in effect after the call. Callers should
// VerifyStatement before recording.
func RecordStatement(stateDir string, s *Statement) (int64, error) {
	prev, err := ReadLastSeenVersion(stateDir, s.Channel)
	if err != nil {
		return 0, err
	}
	if s.Version <= prev {
		return prev, nil
	}
	path, err := channelStatePath(stateDir, s.Channel)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, fmt.Errorf("rulesign: create channel state dir: %w", err)
	}
	// expiresRaw is the exact RFC3339 string from the verified statement (empty
	// only for directly-constructed test statements); storing it verbatim avoids
	// any reformat drift in the offline staleness check.
	data, err := json.Marshal(channelState{Version: s.Version, Digest: s.Digest, Expires: s.expiresRaw})
	if err != nil {
		return 0, fmt.Errorf("rulesign: marshal channel state: %w", err)
	}
	// Write atomically via a UNIQUE temp file then rename: a torn write to the
	// rollback floor must not corrupt it, and a fixed temp name would let two
	// concurrent recorders for the same channel truncate each other's in-progress
	// temp and promote a garbled state. A per-write temp (like cache.go's
	// writeCurrent) isolates concurrent recorders.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-channel-*")
	if err != nil {
		return 0, fmt.Errorf("rulesign: create channel state temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return 0, fmt.Errorf("rulesign: write channel state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return 0, fmt.Errorf("rulesign: close channel state temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return 0, fmt.Errorf("rulesign: commit channel state: %w", err)
	}
	return s.Version, nil
}
