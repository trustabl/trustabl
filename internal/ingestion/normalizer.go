package ingestion

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/trustabl/karenctl/internal/models"
)

// Normalize walks the source tree and produces a ScanManifest. It does NOT
// parse any source language — that's the analysis layer's job. This is the
// cheap pass: "what files are here, what agent components exist, and which
// integrations does the repo declare?"
//
// Skips common noise (.git, .venv, node_modules, __pycache__, dist, build,
// .tox, .mypy_cache, .pytest_cache) and any other dot-prefixed directory,
// EXCEPT .claude/ — that's a real agent-config directory we deliberately
// descend into.
func Normalize(src *Source) (models.ScanManifest, error) {
	manifest := models.ScanManifest{
		RepoRoot:    src.RootPath,
		IsRemote:    src.IsRemote,
		RemoteURL:   src.RemoteURL,
		PythonFiles: []string{},
		YAMLFiles:   []string{},
	}

	err := filepath.WalkDir(src.RootPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(src.RootPath, path)
		if err != nil {
			return nil
		}
		// Normalize to forward slashes so manifest paths are platform-stable
		// (matters on Windows; everything downstream sees POSIX separators).
		rel = filepath.ToSlash(rel)

		switch {
		case strings.HasSuffix(path, ".py"):
			manifest.PythonFiles = append(manifest.PythonFiles, rel)
		case strings.HasSuffix(path, ".ts"), strings.HasSuffix(path, ".tsx"):
			manifest.TypeScriptFiles = append(manifest.TypeScriptFiles, rel)
		case strings.HasSuffix(path, ".js"), strings.HasSuffix(path, ".jsx"), strings.HasSuffix(path, ".mjs"):
			manifest.JavaScriptFiles = append(manifest.JavaScriptFiles, rel)
		case strings.HasSuffix(path, ".yaml"), strings.HasSuffix(path, ".yml"):
			manifest.YAMLFiles = append(manifest.YAMLFiles, rel)
		case strings.HasSuffix(path, ".json"):
			manifest.JSONFiles = append(manifest.JSONFiles, rel)
		case strings.HasSuffix(path, ".md"):
			manifest.MarkdownFiles = append(manifest.MarkdownFiles, rel)
		}
		return nil
	})
	if err != nil {
		return manifest, err
	}

	manifest.HasClaudeSDKDependency = detectClaudeSDKDependency(src.RootPath)
	manifest.HasOpenShellArtifact = detectOpenShellArtifact(manifest.YAMLFiles, src.RootPath)
	manifest.Components = discoverComponents(src.RootPath, manifest)
	return manifest, nil
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".venv", "venv", "node_modules", "__pycache__",
		"dist", "build", ".tox", ".mypy_cache", ".pytest_cache":
		return true
	case ".claude":
		// Agent-config directory — deliberately descend into it.
		return false
	}
	// Skip other dot-prefixed dirs (build caches, editor metadata, etc.).
	if strings.HasPrefix(name, ".") {
		return true
	}
	return false
}

// detectClaudeSDKDependency: cheap text scan of pyproject.toml / requirements.txt
// for the Anthropic SDK marker. This is intentionally fuzzy — false positives are
// cheaper than false negatives at the manifest stage; detectors will be precise.
func detectClaudeSDKDependency(root string) bool {
	candidates := []string{
		filepath.Join(root, "pyproject.toml"),
		filepath.Join(root, "requirements.txt"),
		filepath.Join(root, "Pipfile"),
		filepath.Join(root, "poetry.lock"),
	}
	needles := []string{
		"claude-agent-sdk",
		"claude_agent_sdk",
		"anthropic-agent",
		"anthropic[agent",
	}
	for _, p := range candidates {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		s := strings.ToLower(string(b))
		for _, n := range needles {
			if strings.Contains(s, n) {
				return true
			}
		}
	}
	return false
}

// detectOpenShellArtifact: presence of an openshell/ directory or a *.yaml that
// declares an OpenShell schema. Used to set HasOpenShellArtifact so we can warn
// the user before overwriting an existing policy.
func detectOpenShellArtifact(yamls []string, root string) bool {
	for _, y := range yamls {
		if strings.HasPrefix(y, "openshell/") {
			return true
		}
		b, err := os.ReadFile(filepath.Join(root, y))
		if err != nil {
			continue
		}
		if strings.Contains(string(b), "openshell.nvidia.com/v") {
			return true
		}
	}
	return false
}

// discoverComponents enumerates non-tool agent artifacts from the manifest.
// These are surfaced for context (the user sees the full agent surface) and
// for future detection passes; today's rule engine ignores them.
//
// Patterns are deliberately conservative — better to under-detect a component
// than to mislabel a generic file as agent infrastructure.
func discoverComponents(root string, m models.ScanManifest) []models.AgentComponent {
	var out []models.AgentComponent

	// MCP configs: well-known names at any depth.
	mcpNames := map[string]bool{
		"mcp.json":                   true,
		"mcp_servers.json":           true,
		"claude_desktop_config.json": true,
	}
	for _, p := range m.JSONFiles {
		if mcpNames[filepath.Base(p)] {
			out = append(out, models.AgentComponent{Kind: models.ComponentMCPConfig, Path: p})
		}
	}

	// CLAUDE.md (system-prompt / context for Claude Code agents). Match the
	// canonical name and the lower-case variant; .md base name only.
	for _, p := range m.MarkdownFiles {
		base := filepath.Base(p)
		if base == "CLAUDE.md" || base == "claude.md" {
			out = append(out, models.AgentComponent{Kind: models.ComponentClaudeMd, Path: p})
		}
	}

	// .claude/ subtree: settings, agents, commands.
	for _, p := range m.JSONFiles {
		base := filepath.Base(p)
		if strings.HasPrefix(p, ".claude/") &&
			(base == "settings.json" || base == "settings.local.json") {
			out = append(out, models.AgentComponent{Kind: models.ComponentClaudeSettings, Path: p})
		}
	}
	for _, p := range m.MarkdownFiles {
		switch {
		case strings.HasPrefix(p, ".claude/agents/"):
			out = append(out, models.AgentComponent{Kind: models.ComponentSubagent, Path: p})
		case strings.HasPrefix(p, ".claude/commands/"):
			out = append(out, models.AgentComponent{Kind: models.ComponentSlashCommand, Path: p})
		}
	}

	// Hook scripts: anything under hooks/ (any extension we recognize).
	addHook := func(p string, lang models.Language) {
		if strings.HasPrefix(p, "hooks/") {
			out = append(out, models.AgentComponent{
				Kind: models.ComponentHookScript, Path: p, Language: lang,
			})
		}
	}
	for _, p := range m.PythonFiles {
		addHook(p, models.LanguagePython)
	}
	for _, p := range m.TypeScriptFiles {
		addHook(p, models.LanguageTypeScript)
	}
	for _, p := range m.JavaScriptFiles {
		addHook(p, models.LanguageJavaScript)
	}

	// Sandbox policies: openshell/*.yaml or *.yml.
	for _, p := range m.YAMLFiles {
		if strings.HasPrefix(p, "openshell/") {
			out = append(out, models.AgentComponent{Kind: models.ComponentSandboxPolicy, Path: p})
		}
	}

	// System prompts: prompts/*.md or system_prompt.{txt,md} at root.
	for _, p := range m.MarkdownFiles {
		if strings.HasPrefix(p, "prompts/") || filepath.Base(p) == "system_prompt.md" {
			out = append(out, models.AgentComponent{Kind: models.ComponentSystemPrompt, Path: p})
		}
	}
	if exists(filepath.Join(root, "system_prompt.txt")) {
		out = append(out, models.AgentComponent{Kind: models.ComponentSystemPrompt, Path: "system_prompt.txt"})
	}

	// Dependency manifests at repo root only.
	depFiles := map[string]models.Language{
		"pyproject.toml":   models.LanguagePython,
		"requirements.txt": models.LanguagePython,
		"Pipfile":          models.LanguagePython,
		"poetry.lock":      models.LanguagePython,
		"package.json":     models.LanguageTypeScript, // best-guess; could be JS too
		"go.mod":           models.LanguageGo,
	}
	for name, lang := range depFiles {
		if exists(filepath.Join(root, name)) {
			out = append(out, models.AgentComponent{
				Kind: models.ComponentDependencyManifest, Path: name, Language: lang,
			})
		}
	}

	// Determinism: stable byte-output across runs requires sorted components.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
