package enrichment

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/trustabl/trustabl/internal/analysis/astutil"
)

// validateSyntax reports whether content parses cleanly as the language
// implied by filePath's extension. An extension with no check at all returns
// nil (skip, don't fail closed) rather than rejecting a language this check
// can't judge.
func validateSyntax(ctx context.Context, filePath, content string) error {
	if strings.ToLower(filepath.Ext(filePath)) == ".py" {
		// tree-sitter's grammar has no rule against a call repeating a
		// keyword argument — that's a CPython compile-time check, not a
		// parse-tree shape, so HasError() misses it (a real generated fix
		// duplicated an entire argument list instead of adding one keyword
		// and tree-sitter waved it through). compile() is a strict superset
		// of what the grammar checks, so where it's available it replaces
		// tree-sitter for Python rather than supplementing it. Same
		// technique as trustabl-service's ApplyEnrichment syntax check
		// (pr.go's runPyCompile).
		if err, ok := validatePythonCompile(ctx, content); ok {
			return err
		}
		// No python interpreter on PATH — fall back to tree-sitter below
		// rather than skipping validation for Python entirely.
	}

	var parser *sitter.Parser
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".py":
		parser = astutil.NewPyParser()
	case ".go":
		parser = astutil.NewGoParser()
	case ".ts":
		parser = astutil.NewTSParser()
	case ".tsx":
		parser = astutil.NewTSXParser()
	case ".cs":
		parser = astutil.NewCSharpParser()
	case ".php":
		parser = astutil.NewPHPParser()
	case ".rs":
		parser = astutil.NewRustParser()
	default:
		return nil
	}

	tree, err := astutil.ParseCtxTimeout(ctx, parser, []byte(content))
	if err != nil {
		return fmt.Errorf("parse failed: %w", err)
	}
	if tree.RootNode().HasError() {
		return fmt.Errorf("generated replacement does not parse as valid %s", filepath.Ext(filePath))
	}
	return nil
}

// validatePythonCompile compiles content with a real Python interpreter's
// compile() builtin — in-memory only, no file written to disk. ok is false
// when no python3/python interpreter is on PATH, telling the caller to fall
// back to the tree-sitter check instead; ok is true otherwise, with err
// giving the actual verdict (nil = valid).
func validatePythonCompile(ctx context.Context, content string) (err error, ok bool) {
	pythonBin := "python3"
	if _, lookErr := exec.LookPath(pythonBin); lookErr != nil {
		pythonBin = "python"
		if _, lookErr := exec.LookPath(pythonBin); lookErr != nil {
			return nil, false
		}
	}

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, pythonBin, "-c",
		"import sys\ncompile(sys.stdin.read(), '<enrichment>', 'exec')\n")
	cmd.Stdin = strings.NewReader(content)
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		return fmt.Errorf("generated replacement is not valid Python: %s", strings.TrimSpace(stderr.String())), true
	}
	return nil, true
}
