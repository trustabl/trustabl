package analysis

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSecret(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDiscoverSecrets_LiteralCredential proves a file containing a hardcoded
// GitHub PAT fires SECRET-LIT-001, and the line number is correct.
func TestDiscoverSecrets_LiteralCredential(t *testing.T) {
	root := t.TempDir()
	writeSecret(t, root, "src/tool.py", "# tool implementation\nTOKEN = \"ghp_abcdefghijklmnopqrstuvwxyz1234567890\"\n")
	got := DiscoverSecrets(root)
	if len(got) == 0 {
		t.Fatal("expected SECRET-LIT-001 match, got none")
	}
	found := false
	for _, m := range got {
		if m.RuleID == "SECRET-LIT-001" && m.File == "src/tool.py" {
			found = true
			if m.Line != 2 {
				t.Errorf("expected line 2, got %d", m.Line)
			}
		}
	}
	if !found {
		t.Errorf("no SECRET-LIT-001 for src/tool.py: %+v", got)
	}
}

// TestDiscoverSecrets_ScriptReadsEnv proves a shell script that reads a known
// credential environment variable fires SECRET-ENV-001.
func TestDiscoverSecrets_ScriptReadsEnv(t *testing.T) {
	root := t.TempDir()
	writeSecret(t, root, "hooks/run.sh", "#!/bin/bash\necho $ANTHROPIC_API_KEY\n")
	got := DiscoverSecrets(root)
	if len(got) == 0 {
		t.Fatal("expected SECRET-ENV-001 match, got none")
	}
	found := false
	for _, m := range got {
		if m.RuleID == "SECRET-ENV-001" && m.File == "hooks/run.sh" {
			found = true
		}
	}
	if !found {
		t.Errorf("no SECRET-ENV-001 for hooks/run.sh: %+v", got)
	}
}

// TestDiscoverSecrets_BinarySkipped proves a file with a binary extension is
// not scanned even if it contains a matching pattern.
func TestDiscoverSecrets_BinarySkipped(t *testing.T) {
	root := t.TempDir()
	// Write a .wasm file that would match the literal pattern.
	writeSecret(t, root, "dist/app.wasm", "AKIA1234567890ABCDEF")
	got := DiscoverSecrets(root)
	for _, m := range got {
		if m.File == "dist/app.wasm" {
			t.Errorf("binary file was scanned: %+v", m)
		}
	}
}

// TestDiscoverSecrets_VendoredDirSkipped proves node_modules is not scanned.
func TestDiscoverSecrets_VendoredDirSkipped(t *testing.T) {
	root := t.TempDir()
	writeSecret(t, root, "node_modules/evil/index.js", "const tok = \"ghp_abcdefghijklmnopqrstuvwxyz1234567890\";")
	got := DiscoverSecrets(root)
	for _, m := range got {
		if m.File == "node_modules/evil/index.js" {
			t.Errorf("vendored dir was scanned: %+v", m)
		}
	}
}

// TestDiscoverSecrets_CommentStrippedForEnvCheck proves that a shell env
// variable reference inside a comment does NOT fire SECRET-ENV-001 (comments
// are stripped before the behavioral check) but WOULD fire SECRET-LIT-001 only
// if a real literal pattern is present.
func TestDiscoverSecrets_CommentStrippedForEnvCheck(t *testing.T) {
	root := t.TempDir()
	// Comment-only reference to $ANTHROPIC_API_KEY — should not fire ENV-001.
	writeSecret(t, root, "scripts/deploy.sh", "#!/bin/bash\n# export ANTHROPIC_API_KEY=...\necho hello\n")
	got := DiscoverSecrets(root)
	for _, m := range got {
		if m.File == "scripts/deploy.sh" && m.RuleID == "SECRET-ENV-001" {
			t.Errorf("comment-only env ref should not fire SECRET-ENV-001: %+v", m)
		}
	}
}

// TestDiscoverSecrets_Deterministic proves the result is byte-stable regardless
// of which files are written first.
func TestDiscoverSecrets_Deterministic(t *testing.T) {
	root := t.TempDir()
	writeSecret(t, root, "a/tool.py", "key = \"ghp_abcdefghijklmnopqrstuvwxyz1234567890\"\n")
	writeSecret(t, root, "b/tool.py", "key = \"ghp_abcdefghijklmnopqrstuvwxyz1234567890\"\n")
	first := DiscoverSecrets(root)
	second := DiscoverSecrets(root)
	if len(first) != len(second) {
		t.Fatalf("non-deterministic result lengths: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("non-deterministic at index %d: %+v vs %+v", i, first[i], second[i])
		}
	}
}
