package rulesource

import (
	"io/fs"

	"gopkg.in/yaml.v3"
)

// manifest is the parsed shape of a rule pack's root manifest.yaml.
type manifest struct {
	SchemaVersion int `yaml:"schema_version"`
}

// manifestInfo is the compatibility read of a pack's manifest.yaml.
type manifestInfo struct {
	version int  // declared schema_version (meaningful only when valid)
	valid   bool // manifest present, parseable, and version > 0
}

// readManifestInfo reads and validates a pack's manifest. A missing, malformed,
// or non-positive-version manifest is reported invalid — a pack the engine
// cannot vouch for is not used.
//
// A valid manifest whose version EXCEEDS the engine's supported version is
// still reported valid: the engine no longer refuses a newer pack wholesale.
// The lenient runtime loader (rules.LoadLenient) skips the individual rules it
// cannot evaluate and the scan proceeds. The caller compares version against
// the supported version to set Resolved.SchemaNewer.
func readManifestInfo(fsys fs.FS) manifestInfo {
	b, err := fs.ReadFile(fsys, "manifest.yaml")
	if err != nil {
		return manifestInfo{}
	}
	var m manifest
	if err := yaml.Unmarshal(b, &m); err != nil {
		return manifestInfo{}
	}
	if m.SchemaVersion <= 0 {
		return manifestInfo{}
	}
	return manifestInfo{version: m.SchemaVersion, valid: true}
}
