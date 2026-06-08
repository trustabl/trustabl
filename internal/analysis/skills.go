package analysis

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/trustabl/trustabl/internal/models"
)

type skillFrontmatter struct {
	Name                   string       `yaml:"name"`
	Description            string       `yaml:"description"`
	AllowedTools           stringOrList `yaml:"allowed-tools"`
	DisallowedTools        stringOrList `yaml:"disallowed-tools"`
	ArgumentHint           string       `yaml:"argument-hint"`
	DisableModelInvocation bool         `yaml:"disable-model-invocation"`
	UserInvocable          *bool        `yaml:"user-invocable"`
	Context                string       `yaml:"context"`
	Agent                  string       `yaml:"agent"`
	Hooks                  yaml.Node    `yaml:"hooks"`
}

// DiscoverSkills parses every SKILL.md in the manifest's markdown files. A skill
// is identified by basename (SKILL.md) at any depth — covering .claude/skills/,
// ~/.claude/skills/ layouts, plugin skills/ dirs, and nested monorepo skills.
// Files without frontmatter or without a name are skipped. The markdown body is
// audited for body facts (dynamic-context shell execution, external URLs,
// injection markers); see parseSkillBody.
func DiscoverSkills(manifest models.ScanManifest) []models.SkillDef {
	var out []models.SkillDef
	for _, p := range manifest.MarkdownFiles {
		if filepath.Base(p) != "SKILL.md" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(manifest.RepoRoot, p))
		if err != nil {
			continue
		}
		fm, body, startLine, endLine, ok := extractFrontmatterAndBody(raw)
		if !ok {
			continue
		}
		var parsed skillFrontmatter
		if err := yaml.Unmarshal(fm, &parsed); err != nil {
			continue
		}
		if parsed.Name == "" {
			continue
		}
		tokens := splitToolsTokens([]string(parsed.AllowedTools))
		dynExec, urls, markers := parseSkillBody(string(body))
		out = append(out, models.SkillDef{
			Name:                   parsed.Name,
			Description:            parsed.Description,
			AllowedTools:           tokens,
			ToolGrants:             parseToolGrants(tokens),
			DisallowedTools:        splitToolsTokens([]string(parsed.DisallowedTools)),
			ArgumentHint:           parsed.ArgumentHint,
			DisableModelInvocation: parsed.DisableModelInvocation,
			UserInvocable:          parsed.UserInvocable,
			Context:                parsed.Context,
			Agent:                  parsed.Agent,
			HasHooks:               parsed.Hooks.Kind == yaml.MappingNode && len(parsed.Hooks.Content) > 0,
			DynamicExecCommands:    dynExec,
			ExternalURLs:           urls,
			InjectionMarkers:       markers,
			BundledFiles:           bundledFiles(manifest.RepoRoot, p),
			Location:               models.Location{FilePath: p, Line: startLine, EndLine: endLine},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FilePath < out[j].FilePath })
	return out
}

var (
	// skillInlineExecRe matches the inline dynamic-context form !`cmd`. Claude
	// Code only recognizes it when the ! is at line start or immediately after
	// whitespace (so KEY=!`cmd` stays literal text, never executed); the
	// (?:^|[ \t]) prefix mirrors that grammar. Group 1 is the command, captured
	// up to the closing backtick on the same line.
	skillInlineExecRe = regexp.MustCompile("(?m)(?:^|[ \\t])!`([^`\\n]+)`")
	// skillExternalURLRe matches http(s) URLs in the body, stopping at
	// whitespace or common closing/markdown punctuation.
	skillExternalURLRe = regexp.MustCompile(`https?://[^\s)>\]"'` + "`" + `]+`)
	// skillInjectionPhraseRe matches instruction-override phrasing characteristic
	// of prompt-injection payloads ("ignore the previous instructions", etc.).
	skillInjectionPhraseRe = regexp.MustCompile(`(?i)(?:ignore|disregard|forget)\s+(?:all\s+|any\s+)?(?:the\s+)?(?:previous|prior|earlier|above)\s+(?:instructions?|prompts?|context|messages?)`)
	// skillBase64BlobRe matches a long unbroken base64 run — a common carrier for
	// obfuscated injected instructions. The 160-char floor keeps ordinary hashes,
	// tokens, and short data URIs from tripping this (low-confidence) signal.
	skillBase64BlobRe = regexp.MustCompile(`[A-Za-z0-9+/]{160,}={0,2}`)
)

// zeroWidthChars are invisible code points used to smuggle injected instructions
// past human review: zero-width space, zero-width non-joiner, zero-width joiner,
// and the byte-order mark.
const zeroWidthChars = "\u200b\u200c\u200d\ufeff"

// parseSkillBody extracts security-relevant facts from the markdown body of a
// SKILL.md (everything below the frontmatter). Output is deterministic: commands
// and URLs are returned in first-seen order with duplicates removed; markers are
// fixed symbolic tokens. See models.SkillDef for field semantics.
func parseSkillBody(body string) (dynExec, urls, markers []string) {
	// Fenced ```! blocks: every non-blank line until the closing fence is a
	// pre-model shell command. Scanned first, so a multi-line block is captured
	// ahead of the inline pass.
	inExecFence := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case inExecFence && strings.HasPrefix(trimmed, "```"):
			inExecFence = false
		case inExecFence:
			if trimmed != "" {
				dynExec = append(dynExec, trimmed)
			}
		case strings.HasPrefix(trimmed, "```!"):
			inExecFence = true
		}
	}
	// Inline !`cmd` form.
	for _, m := range skillInlineExecRe.FindAllStringSubmatch(body, -1) {
		dynExec = append(dynExec, strings.TrimSpace(m[1]))
	}
	urls = skillExternalURLRe.FindAllString(body, -1)
	if skillInjectionPhraseRe.MatchString(body) {
		markers = append(markers, "instruction-override-phrase")
	}
	if strings.ContainsAny(body, zeroWidthChars) {
		markers = append(markers, "zero-width-characters")
	}
	if skillBase64BlobRe.MatchString(body) {
		markers = append(markers, "long-base64-blob")
	}
	return dedupeStrings(dynExec), dedupeStrings(urls), markers
}

// dedupeStrings returns in with duplicates removed, preserving first-seen order.
// Returns nil for an empty result so SkillDef slice fields stay omitempty-clean.
func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// bundledFiles inventories the non-SKILL.md files shipped alongside a skill, by
// walking the skill's own directory (filepath.Dir of the SKILL.md path) under
// repoRoot. A skill whose SKILL.md sits at the repo root has no bounded skill
// directory, so it returns nil rather than walking the whole repository. Any
// SKILL.md (the entrypoint, or a nested skill's) is skipped. Paths are
// repo-relative and slash-separated; the result is sorted for determinism.
func bundledFiles(repoRoot, skillPath string) []models.BundledFile {
	dir := filepath.Dir(skillPath)
	if dir == "." || dir == "" {
		return nil
	}
	var out []models.BundledFile
	_ = filepath.WalkDir(filepath.Join(repoRoot, dir), func(abs string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() == "SKILL.md" {
			return nil
		}
		rel, rerr := filepath.Rel(repoRoot, abs)
		if rerr != nil {
			return nil
		}
		out = append(out, models.BundledFile{Path: filepath.ToSlash(rel), Kind: classifyBundledFile(d.Name())})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// classifyBundledFile buckets a bundled file by extension. "script" is the
// security-relevant bucket: code a skill can execute via bash. Classification is
// extension-only (no content sniffing), so an extension-less script reads as
// "resource" — a known v1 gap.
func classifyBundledFile(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".sh", ".bash", ".zsh", ".fish", ".py", ".js", ".mjs", ".cjs",
		".ts", ".rb", ".pl", ".php", ".ps1", ".bat", ".cmd", ".lua", ".r":
		return "script"
	case ".md", ".markdown":
		return "markdown"
	case ".exe", ".dll", ".so", ".dylib", ".bin", ".wasm", ".node",
		".o", ".a", ".class", ".pyc":
		return "binary"
	default:
		return "resource"
	}
}
