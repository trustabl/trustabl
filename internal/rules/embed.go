package rules

import (
	"embed"
	"io/fs"
)

// embeddedPolicies holds the YAML rule definitions baked into the binary.
// Use the all: prefix so go:embed pulls every file recursively, including
// dotfiles (none today, but the pattern is forgiving for future additions).
//
//go:embed all:policies
var embeddedPolicies embed.FS

// DefaultFS returns the built-in policies as an fs.FS rooted at the policies
// directory. The "policies/" prefix is stripped so callers see paths like
// "claude_sdk/network.yaml" rather than "policies/claude_sdk/network.yaml".
//
// Pair with Load or LoadRegistry:
//
//	registry, err := rules.LoadRegistry(rules.DefaultFS())
func DefaultFS() fs.FS {
	sub, err := fs.Sub(embeddedPolicies, "policies")
	if err != nil {
		// Unreachable: "policies" is a statically-embedded directory.
		panic(err)
	}
	return sub
}
