package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
)

func TestDiscoverTSOpenAISessions_ThreeClasses(t *testing.T) {
	src := `
import { MemorySession } from "@openai/agents";
import { OpenAIConversationsSession, OpenAIResponsesCompactionSession } from "@openai/agents-openai";

const mem  = new MemorySession();
const conv = new OpenAIConversationsSession({ conversationId: "x" });
const comp = new OpenAIResponsesCompactionSession({});
`
	pf := parseTSForTest(t, "src/s.ts", src)
	got := analysis.DiscoverTSOpenAISessions([]analysis.ParsedFile{pf}, nil)
	if len(got) != 3 {
		t.Fatalf("got %d, want 3: %+v", len(got), got)
	}
	classes := map[string]bool{}
	for _, s := range got {
		classes[s.Class] = true
	}
	for _, want := range []string{"MemorySession", "OpenAIConversationsSession", "OpenAIResponsesCompactionSession"} {
		if !classes[want] {
			t.Errorf("missing session class %q", want)
		}
	}
}

func TestDiscoverTSOpenAISessions_FactoryFunction(t *testing.T) {
	src := `
import { startOpenAIConversationsSession } from "@openai/agents-openai";
const conv = await startOpenAIConversationsSession({});
`
	pf := parseTSForTest(t, "src/s.ts", src)
	got := analysis.DiscoverTSOpenAISessions([]analysis.ParsedFile{pf}, nil)
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
	if got[0].Class != "startOpenAIConversationsSession" {
		t.Errorf("Class = %q", got[0].Class)
	}
}

func TestDiscoverTSOpenAISessions_NoImportGate(t *testing.T) {
	src := `
class MemorySession {}
const x = new MemorySession();
`
	pf := parseTSForTest(t, "src/s.ts", src)
	got := analysis.DiscoverTSOpenAISessions([]analysis.ParsedFile{pf}, nil)
	if len(got) != 0 {
		t.Errorf("no-SDK-import should yield zero, got %+v", got)
	}
}
