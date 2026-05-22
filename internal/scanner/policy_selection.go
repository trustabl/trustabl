package scanner

import (
	"fmt"

	"github.com/trustabl/trustabl/internal/models"
)

// shippedPolicySDKs lists the SDKs we have policy packs for.
var shippedPolicySDKs = map[models.SDK]bool{
	models.SDKClaudeAgentSDK: true,
	models.SDKOpenAIAgents:   true,
	models.SDKMCP:            true,
	models.SDKOpenShell:      true,
}

// depNameToSDK maps the canonical dep-file package name to the SDK enum.
var depNameToSDK = map[string]models.SDK{
	"claude-agent-sdk": models.SDKClaudeAgentSDK,
	"openai-agents":    models.SDKOpenAIAgents,
}

// SelectAndEmitMETA inspects the profile + inventory and emits engine-level
// info findings:
//
//	META-001 — an SDK observed in code is not currently audited
//	META-002 — an SDK declared as a dep has no observed code use
//	META-003 — an agent has opaque configuration (Agent(**...) or non-list tools=)
func SelectAndEmitMETA(profile models.RepoProfile, inv models.RepoInventory) []models.Finding {
	var out []models.Finding

	// META-001: unaudited SDKs observed in code
	for _, sdk := range inv.SDKsDetected {
		if !shippedPolicySDKs[sdk] {
			out = append(out, models.Finding{
				RuleID:   "META-001",
				Severity: models.SeverityInfo,
				Title:    "Unaudited SDK in use",
				Explanation: fmt.Sprintf(
					"This repo uses SDK %q, which Trustabl does not currently audit. "+
						"No rules will fire against agents or tools from this SDK.", sdk),
				SuggestedFix: "If detection for this SDK is needed, file an issue or contribute a policy pack under internal/rules/policies/<sdk>/.",
				Confidence:   1.0,
			})
		}
	}

	// META-002: declared deps with no observed code use
	observed := make(map[models.SDK]bool)
	for _, s := range inv.SDKsDetected {
		observed[s] = true
	}
	seenDrift := make(map[string]bool)
	for _, dep := range profile.SDKDeps {
		sdk, known := depNameToSDK[dep.Name]
		if !known {
			continue
		}
		if observed[sdk] {
			continue
		}
		if seenDrift[dep.Name] {
			continue
		}
		seenDrift[dep.Name] = true
		out = append(out, models.Finding{
			RuleID:   "META-002",
			Severity: models.SeverityInfo,
			Title:    "Declared SDK dependency has no observed code use",
			Explanation: fmt.Sprintf(
				"The project declares %q as a dependency (in %s) but Trustabl found no "+
					"code that uses it. The corresponding rules will not fire until an "+
					"agent or tool from this SDK appears in code.", dep.Name, dep.Source),
			SuggestedFix: "If the dep was added intentionally for non-agent reasons, suppress this finding. Otherwise, remove the unused dep.",
			Confidence:   1.0,
		})
	}

	// META-003: opaque agents
	for _, a := range inv.Agents {
		if !a.Opaque {
			continue
		}
		out = append(out, models.Finding{
			RuleID:   "META-003",
			Severity: models.SeverityInfo,
			FilePath: a.FilePath,
			Line:     a.Line,
			Title:    "Agent configuration is opaque",
			Explanation: "Agent configuration is opaque (kwargs come from a variable via **unpack, " +
				"or tools= is a non-literal expression like a function call); rules cannot evaluate against this agent.",
			SuggestedFix: "Inline the agent's kwargs at the constructor call site, or move the dynamic configuration into explicit code that Trustabl can analyze.",
			Confidence:   1.0,
		})
	}

	return out
}

// sdkToCategory maps an observed agent SDK to the detector category that
// audits it. Only SDKs that ship a policy pack with a "false clean bill"
// risk are listed; openshell is always-on and MCP has no agent surface.
var sdkToCategory = map[models.SDK]models.DetectorCategory{
	models.SDKClaudeAgentSDK: models.CategoryClaudeSDK,
	models.SDKOpenAIAgents:   models.CategoryOpenAISDK,
}

// EmitCoverageMETA emits META-004 when an audited SDK was observed in code
// but not a single one of its loaded rules was even applicable to anything
// discovered — i.e. trustabl could not actually audit it, yet the absence of
// findings would otherwise read as a clean bill of health. `applicable` is
// the set of categories that had at least one detector Apply to at least one
// entity (Registry.ApplicableCategories).
func EmitCoverageMETA(applicable map[models.DetectorCategory]bool, inv models.RepoInventory) []models.Finding {
	var out []models.Finding
	for _, sdk := range inv.SDKsDetected {
		cat, known := sdkToCategory[sdk]
		if !known || applicable[cat] {
			continue
		}
		out = append(out, models.Finding{
			RuleID:   "META-004",
			Severity: models.SeverityInfo,
			Title:    "SDK detected but no rule was applicable",
			Explanation: fmt.Sprintf(
				"Trustabl detected the %q SDK in code and loaded its policy pack, but "+
					"none of that pack's rules were applicable to any discovered tool or "+
					"agent. The absence of findings does NOT mean this code is clean — it "+
					"means Trustabl could not audit it (often because tools are declared in "+
					"a shape discovery does not yet extract).", sdk),
			SuggestedFix: "Treat this scan as uncovered for this SDK, not as a pass. File an issue with the agent/tool shape so discovery can be extended.",
			Confidence:   1.0,
		})
	}
	return out
}
