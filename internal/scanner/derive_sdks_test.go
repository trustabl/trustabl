package scanner

import (
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func hasSDK(sdks []models.SDK, want models.SDK) bool {
	for _, s := range sdks {
		if s == want {
			return true
		}
	}
	return false
}

// TestDeriveSDKsDetected_SettingsPresenceMarksClaude verifies that a repo which
// ships only a .claude/settings.json (no SDK code, no subagents, no tools) is
// still classified as a Claude Agent SDK surface. Without this, the claude_sdk
// pack never loads for settings-only repos and the defaultMode bypass rule
// (CSDK-201) can never fire.
func TestDeriveSDKsDetected_SettingsPresenceMarksClaude(t *testing.T) {
	settings := []models.ClaudeSettings{{DefaultMode: "bypassPermissions"}}
	got := deriveSDKsDetected(nil, nil, nil, settings)
	if !hasSDK(got, models.SDKClaudeAgentSDK) {
		t.Errorf("settings-only repo: SDKsDetected = %v, want it to include %q", got, models.SDKClaudeAgentSDK)
	}
}

// TestDeriveSDKsDetected_NoSettingsNoClaude is the silent case: an OpenAI-only
// repo with no Claude settings must not be misclassified as Claude.
func TestDeriveSDKsDetected_NoSettingsNoClaude(t *testing.T) {
	tools := []models.ToolDef{{Kind: models.KindOpenAITool}}
	got := deriveSDKsDetected(tools, nil, nil, nil)
	if hasSDK(got, models.SDKClaudeAgentSDK) {
		t.Errorf("OpenAI-only repo: SDKsDetected = %v, must not include %q", got, models.SDKClaudeAgentSDK)
	}
	if !hasSDK(got, models.SDKOpenAIAgents) {
		t.Errorf("OpenAI-only repo: SDKsDetected = %v, want %q", got, models.SDKOpenAIAgents)
	}
}
