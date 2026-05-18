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
					"This repo uses SDK %q, which trustabl does not currently audit. "+
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
				"The project declares %q as a dependency (in %s) but trustabl found no "+
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
			SuggestedFix: "Inline the agent's kwargs at the constructor call site, or move the dynamic configuration into explicit code that trustabl can analyze.",
			Confidence:   1.0,
		})
	}

	return out
}
