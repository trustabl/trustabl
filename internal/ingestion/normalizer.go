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
// onFile, when non-nil, is invoked once per file the recon pass touches — both
// as the tree is walked and as file bodies are read during component discovery.
// It is a progress hook only; it must not influence the returned profile.
func Recon(src *Source, onFile func(string)) (models.RepoProfile, error) {
	manifest, err := Normalize(src, onFile)
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
		{Name: "google-adk", Pattern: "@google/adk",
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
// maxScannedFileBytes caps the size of an individual source file the scan will
// consider. A single multi-gigabyte file (hostile, or a generated blob checked
// into the repo) must not be slurped into memory by a downstream reader and
// OOM the scan. Files over this cap are left out of the manifest entirely, so
// no reader ever opens them. 10 MiB comfortably covers real source files.
const maxScannedFileBytes = 10 << 20

func Normalize(src *Source, onFile func(string)) (models.ScanManifest, error) {
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
		// Skip symlinks: following one can read content outside the repo tree
		// (e.g. a link to /etc/passwd or ../../secret), whose bytes are not part
		// of the repo being scanned and could surface in a finding snippet.
		// WalkDir does not descend symlinked directories, but it still yields
		// symlinked files — exclude them here so no downstream reader follows them.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		// Cap individual file size so a giant file cannot OOM a downstream reader.
		if info, ierr := d.Info(); ierr == nil && info.Size() > maxScannedFileBytes {
			return nil
		}
		rel, err := filepath.Rel(src.RootPath, path)
		if err != nil {
			return nil
		}
		// Normalize to forward slashes so manifest paths are platform-stable
		// (matters on Windows; everything downstream sees POSIX separators).
		rel = filepath.ToSlash(rel)

		// Progress: report every file the walk visits.
		if onFile != nil {
			onFile(rel)
		}

		// Classify by extension off the slash-normalized rel, lowercased, so an
		// uppercase extension (Agent.PY, Tool.TS, CLAUDE.MD) on a case-insensitive
		// filesystem (Windows/macOS) is still recognized rather than silently
		// dropped from the manifest. Matching the OS-native `path` here was both
		// case-sensitive and inconsistent with the rel that gets stored.
		lower := strings.ToLower(rel)
		switch {
		case strings.HasSuffix(lower, ".py"):
			manifest.PythonFiles = append(manifest.PythonFiles, rel)
		case strings.HasSuffix(lower, ".ts"),
			strings.HasSuffix(lower, ".tsx"),
			strings.HasSuffix(lower, ".mts"),
			strings.HasSuffix(lower, ".cts"):
			manifest.TypeScriptFiles = append(manifest.TypeScriptFiles, rel)
		case strings.HasSuffix(lower, ".js"), strings.HasSuffix(lower, ".jsx"), strings.HasSuffix(lower, ".mjs"):
			manifest.JavaScriptFiles = append(manifest.JavaScriptFiles, rel)
		case strings.HasSuffix(lower, ".yaml"), strings.HasSuffix(lower, ".yml"):
			manifest.YAMLFiles = append(manifest.YAMLFiles, rel)
		case strings.HasSuffix(lower, ".json"):
			manifest.JSONFiles = append(manifest.JSONFiles, rel)
		case strings.HasSuffix(lower, ".md"):
			manifest.MarkdownFiles = append(manifest.MarkdownFiles, rel)
		}
		return nil
	})
	if err != nil {
		return manifest, err
	}

	manifest.HasClaudeSDKDependency = detectClaudeSDKDependency(src.RootPath)
	manifest.HasOpenShellArtifact = detectOpenShellArtifact(manifest.YAMLFiles, src.RootPath)
	manifest.Components = discoverComponents(src.RootPath, manifest, onFile)
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
func discoverComponents(root string, m models.ScanManifest, onFile func(string)) []models.AgentComponent {
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
	// Plugin roots are the directories containing a .claude-plugin/plugin.json.
	// Used by the markdown switch below to recognize plugin-distributed commands
	// at <root>/commands/*.md (the layout used by, e.g., wshobson/agents).
	pluginRoots := pluginRootSet(m.JSONFiles)

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
		case isPluginSlashCommand(p, pluginRoots):
			// Plugin layouts (e.g. wshobson/agents) place commands at
			// <plugin-root>/commands/*.md, not .claude/commands/. Tag them when
			// the parent directory has a sibling .claude-plugin/plugin.json.
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
		// Progress: reading every Python body is the measured slow span of recon
		// on large repos — report each so the counter keeps moving here.
		if onFile != nil {
			onFile(p)
		}
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

// pluginRootSet derives the set of plugin-root directories from the JSON
// file list. A plugin root is the directory containing a
// .claude-plugin/plugin.json file (NOT marketplace.json — that's a catalog,
// not a plugin). Root-level plugin manifests yield the empty string as the
// root. All paths are forward-slash by manifest convention.
func pluginRootSet(jsonFiles []string) map[string]bool {
	roots := make(map[string]bool)
	for _, p := range jsonFiles {
		switch {
		case p == ".claude-plugin/plugin.json":
			roots[""] = true
		case strings.HasSuffix(p, "/.claude-plugin/plugin.json"):
			roots[strings.TrimSuffix(p, "/.claude-plugin/plugin.json")] = true
		}
	}
	return roots
}

// isPluginSlashCommand reports whether forward-slash path p is a direct child
// of a <plugin-root>/commands/ directory. The file must be exactly one level
// below commands/ (no nested subdirectories) so we don't sweep up e.g. shared
// fragments in commands/_partials/.
func isPluginSlashCommand(p string, roots map[string]bool) bool {
	const seg = "/commands/"
	idx := strings.LastIndex(p, seg)
	if idx < 0 {
		// Could still be a root-level "commands/<file>.md" when the plugin
		// manifest sits at the repo root.
		if roots[""] && strings.HasPrefix(p, "commands/") {
			return !strings.Contains(p[len("commands/"):], "/")
		}
		return false
	}
	root := p[:idx]
	if !roots[root] {
		return false
	}
	rest := p[idx+len(seg):]
	return !strings.Contains(rest, "/")
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
