package generation

import (
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/trustabl/karenctl/internal/models"
)

// GeneratePolicy emits openshell/policy.yaml from OSH-* findings.
//
// SCHEMA NOTE: This generator targets a plausible NVIDIA OpenShell schema
// (apiVersion: openshell.nvidia.com/v1, kind: SandboxPolicy). The real OpenShell
// schema must be plugged in once the team has the spec link. The structure
// below is what the generator emits; renames are mechanical.
//
// Disambiguation: this is the user's *runtime sandbox policy*. It is NOT a
// future Rego detection policy that could replace the Go detector logic.
func GeneratePolicy(findings []models.Finding, version string) []models.GeneratedArtifact {
	osh := filterCategory(findings, models.CategoryOpenShell)
	if len(osh) == 0 {
		// Even with no findings we emit a defaults-only policy so the user
		// has a starting file rather than having to author from scratch.
		return []models.GeneratedArtifact{{
			RelativePath: "openshell/policy.yaml",
			Contents:     marshalPolicy(buildDefaultsOnlyPolicy(version)),
			Category:     models.CategoryOpenShell,
			Rationale:    "No OpenShell findings. Emitted defaults-only policy as a starter.",
		}}
	}

	policy := buildPolicy(osh, version)
	rationale := fmt.Sprintf("Generated from %d OpenShell finding(s).", len(osh))
	return []models.GeneratedArtifact{{
		RelativePath: "openshell/policy.yaml",
		Contents:     marshalPolicy(policy),
		Category:     models.CategoryOpenShell,
		Rationale:    rationale,
	}}
}

func filterCategory(findings []models.Finding, cat models.DetectorCategory) []models.Finding {
	var out []models.Finding
	for _, f := range findings {
		if f.Category == cat {
			out = append(out, f)
		}
	}
	return out
}

// ────────────────────────────────────────────────────────────────────────────
// Policy model (mirrors the YAML we emit)
// ────────────────────────────────────────────────────────────────────────────

type policyDoc struct {
	APIVersion string         `yaml:"apiVersion"`
	Kind       string         `yaml:"kind"`
	Metadata   policyMeta     `yaml:"metadata"`
	Spec       policySpec     `yaml:"spec"`
}

type policyMeta struct {
	Name        string `yaml:"name"`
	GeneratedBy string `yaml:"generatedBy"`
}

type policySpec struct {
	Defaults policyDefaults        `yaml:"defaults"`
	Tools    map[string]policyTool `yaml:"tools,omitempty"`
	GlobalDeny []policyDeny         `yaml:"globalDeny,omitempty"`
}

type policyDefaults struct {
	CPU     string `yaml:"cpu"`
	Memory  string `yaml:"memory"`
	Timeout string `yaml:"timeout"`
}

type policyTool struct {
	Filesystem *policyFS      `yaml:"filesystem,omitempty"`
	Network    *policyNet     `yaml:"network,omitempty"`
	Commands   *policyCommands `yaml:"commands,omitempty"`
	Deny       []policyDeny    `yaml:"deny,omitempty"`
}

type policyFS struct {
	WritePrefixes []string `yaml:"writePrefixes"`
}

type policyNet struct {
	AllowedHosts []string `yaml:"allowedHosts"`
}

type policyCommands struct {
	Allowed []string `yaml:"allowed"`
}

type policyDeny struct {
	Reason  string `yaml:"reason"`
	RuleID  string `yaml:"ruleId,omitempty"`
}

// ────────────────────────────────────────────────────────────────────────────
// builders
// ────────────────────────────────────────────────────────────────────────────

func buildDefaultsOnlyPolicy(version string) policyDoc {
	return policyDoc{
		APIVersion: "openshell.nvidia.com/v1",
		Kind:       "SandboxPolicy",
		Metadata: policyMeta{
			Name:        "agent-policy",
			GeneratedBy: "karenctl@" + version,
		},
		Spec: policySpec{
			Defaults: policyDefaults{
				CPU:     "1",
				Memory:  "512Mi",
				Timeout: "30s",
			},
		},
	}
}

func buildPolicy(findings []models.Finding, version string) policyDoc {
	doc := buildDefaultsOnlyPolicy(version)
	doc.Spec.Tools = map[string]policyTool{}

	for _, f := range findings {
		switch f.RuleID {
		case "OSH-001":
			doc.Spec.GlobalDeny = append(doc.Spec.GlobalDeny, policyDeny{
				Reason: "shell=True is forbidden — invocations that use it are rejected at the sandbox layer.",
				RuleID: "OSH-001",
			})
		case "OSH-002":
			t := doc.Spec.Tools[f.ToolName]
			if t.Commands == nil {
				t.Commands = &policyCommands{
					// TODO: replace with the actual commands this tool needs.
					// The placeholders below are intentionally minimal — commit
					// only the binaries you have audited.
					Allowed: []string{"# TODO: list allowed commands, e.g. git, python3"},
				}
			}
			doc.Spec.Tools[f.ToolName] = t
		case "OSH-003":
			t := doc.Spec.Tools[f.ToolName]
			if t.Filesystem == nil {
				t.Filesystem = &policyFS{WritePrefixes: []string{"/tmp/agent"}}
			}
			doc.Spec.Tools[f.ToolName] = t
		case "OSH-004":
			// Resource limits live on defaults; nothing per-tool to add.
		case "OSH-005":
			t := doc.Spec.Tools[f.ToolName]
			if t.Network == nil {
				t.Network = &policyNet{AllowedHosts: []string{"# TODO: enumerate allowed hostnames"}}
			}
			doc.Spec.Tools[f.ToolName] = t
		}
	}

	// De-dupe the GlobalDeny by reason to keep output stable for repeat findings.
	doc.Spec.GlobalDeny = dedupeDeny(doc.Spec.GlobalDeny)
	return doc
}

func dedupeDeny(in []policyDeny) []policyDeny {
	seen := map[string]bool{}
	var out []policyDeny
	for _, d := range in {
		key := d.RuleID + "|" + d.Reason
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RuleID < out[j].RuleID })
	return out
}

func marshalPolicy(doc policyDoc) string {
	b, err := yaml.Marshal(doc)
	if err != nil {
		// yaml.Marshal of a known shape shouldn't fail; if it does, emit a
		// visible error rather than panicking the CLI.
		return fmt.Sprintf("# karenctl: failed to marshal policy: %v\n", err)
	}
	return string(b)
}
