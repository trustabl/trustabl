package ingestion

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/trustabl/trustabl/internal/models"
)

// Recon is the recon-step entrypoint. It walks the source tree (cheap, no AST)
// and returns a typed RepoProfile capturing languages, SDK deps, and the
// existing ScanManifest.
func Recon(src *Source) (models.RepoProfile, error) {
	manifest, err := Normalize(src)
	if err != nil {
		return models.RepoProfile{}, err
	}
	langs := languagesFromManifest(manifest)
	sdks := detectSDKDeps(src.RootPath)
	return models.RepoProfile{
		Languages: langs,
		SDKDeps:   sdks,
		Manifest:  manifest,
	}, nil
}

func languagesFromManifest(m models.ScanManifest) []models.Language {
	var langs []models.Language
	if len(m.PythonFiles) > 0 {
		langs = append(langs, models.LanguagePython)
	}
	if len(m.TypeScriptFiles) > 0 {
		langs = append(langs, models.LanguageTypeScript)
	}
	if len(m.JavaScriptFiles) > 0 {
		langs = append(langs, models.LanguageJavaScript)
	}
	return langs
}

// detectSDKDeps scans pyproject.toml / requirements.txt / Pipfile / poetry.lock /
// package.json for known SDK package names.
func detectSDKDeps(root string) []models.SDKDep {
	type needle struct {
		Name      string
		Pattern   string
		Manifests []string
	}
	needles := []needle{
		// NOTE: "claude-agent-sdk" is a literal substring of the TS package id
		// "@anthropic-ai/claude-agent-sdk". Keep this needle's Manifests list
		// restricted to Python manifests ONLY (pyproject.toml, requirements.txt,
		// Pipfile, poetry.lock). Adding "package.json" here would cause every
		// TS-only Claude SDK repo to spuriously emit a Python "claude-agent-sdk"
		// SDKDep via substring match against the TS needle's text below.
		{Name: "claude-agent-sdk", Pattern: "claude-agent-sdk",
			Manifests: []string{"pyproject.toml", "requirements.txt", "Pipfile", "poetry.lock"}},
		{Name: "claude-agent-sdk", Pattern: "claude_agent_sdk",
			Manifests: []string{"pyproject.toml", "requirements.txt", "Pipfile", "poetry.lock"}},
		{Name: "openai-agents", Pattern: "openai-agents",
			Manifests: []string{"pyproject.toml", "requirements.txt", "Pipfile", "poetry.lock"}},
		{Name: "openai-agents", Pattern: "@openai/agents",
			Manifests: []string{"package.json"}},
		{Name: "google-adk", Pattern: "google-adk",
			Manifests: []string{"pyproject.toml", "requirements.txt", "Pipfile", "poetry.lock"}},
		// TS Claude SDK — restrict to package.json. See the comment above for
		// why this MUST NOT be combined with the Python "claude-agent-sdk" needle.
		{Name: "claude-agent-sdk", Pattern: "@anthropic-ai/claude-agent-sdk",
			Manifests: []string{"package.json"}},
	}
	seen := make(map[string]bool)
	var out []models.SDKDep
	for _, n := range needles {
		for _, mfile := range n.Manifests {
			path := filepath.Join(root, mfile)
			b, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			if strings.Contains(strings.ToLower(string(b)), n.Pattern) {
				key := n.Name + "@" + mfile
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, models.SDKDep{
					Name:       n.Name,
					Source:     mfile,
					Confidence: 0.9,
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Source < out[j].Source
	})
	return out
}

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
		case strings.HasSuffix(path, ".ts"),
			strings.HasSuffix(path, ".tsx"),
			strings.HasSuffix(path, ".mts"),
			strings.HasSuffix(path, ".cts"):
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
	case ".claude", ".claude-plugin":
		// Agent-config directories — deliberately descend into them
		// (.claude/ holds agents/skills/commands/settings; .claude-plugin/
		// holds plugin.json / marketplace.json).
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
//
// NOTE: the needles below are SUBSTRINGS of the TS package id
// "@anthropic-ai/claude-agent-sdk". The candidates list is restricted to
// Python manifests ONLY. Do NOT add package.json here — every TS-only Claude
// SDK repo would spuriously set HasClaudeSDKDependency=true via substring.
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

	// .claude/ subtree: settings, agents, commands. Match the .claude/<kind>/
	// segment at ANY depth, not just repo root — monorepos nest agent
	// projects (e.g. agent/.claude/agents/, packages/x/.claude/agents/).
	for _, p := range m.JSONFiles {
		base := filepath.Base(p)
		if hasClaudeSegment(p, "") &&
			(base == "settings.json" || base == "settings.local.json") {
			out = append(out, models.AgentComponent{Kind: models.ComponentClaudeSettings, Path: p})
		}
	}
	for _, p := range m.MarkdownFiles {
		switch {
		case filepath.Base(p) == "SKILL.md":
			// Skills: SKILL.md at any depth (.claude/skills/<name>/SKILL.md,
			// plugin skills/, nested monorepo skills). Identified by basename.
			out = append(out, models.AgentComponent{Kind: models.ComponentSkill, Path: p})
		case hasClaudeSegment(p, "agents/"):
			out = append(out, models.AgentComponent{Kind: models.ComponentSubagent, Path: p})
		case hasClaudeSegment(p, "commands/"):
			out = append(out, models.AgentComponent{Kind: models.ComponentSlashCommand, Path: p})
		}
	}

	// Plugin manifests under .claude-plugin/ (plugin.json or marketplace.json).
	for _, p := range m.JSONFiles {
		base := filepath.Base(p)
		if (base == "plugin.json" || base == "marketplace.json") &&
			(strings.HasPrefix(p, ".claude-plugin/") || strings.Contains(p, "/.claude-plugin/")) {
			out = append(out, models.AgentComponent{Kind: models.ComponentPluginManifest, Path: p})
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

	// Claude Agent SDK AgentDefinition usage. Scan each Python file for a
	// constructor call to AgentDefinition; this is the cheapest reliable
	// signal of subagent composition, which our tool-decorator discovery
	// otherwise misses entirely. Substring-only — we accept a small false-
	// positive risk (e.g. a comment containing "AgentDefinition(") in
	// exchange for not parsing every Python file twice.
	for _, p := range m.PythonFiles {
		b, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			continue
		}
		s := string(b)
		if strings.Contains(s, "AgentDefinition(") && strings.Contains(s, "claude_agent_sdk") {
			out = append(out, models.AgentComponent{
				Kind: models.ComponentClaudeAgentDefinition, Path: p, Language: models.LanguagePython,
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

// hasClaudeSegment reports whether forward-slash path p contains a
// ".claude/<sub>" segment at any depth (repo root or nested under a project
// dir). sub is "" to match the .claude/ dir itself (for files like
// settings.json directly in .claude/), or "agents/" / "commands/" to match a
// specific subdirectory.
func hasClaudeSegment(p, sub string) bool {
	seg := ".claude/" + sub
	return strings.HasPrefix(p, seg) || strings.Contains(p, "/"+seg)
}
