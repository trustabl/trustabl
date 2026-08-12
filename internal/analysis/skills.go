package analysis

import (
	"io"
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
		dynExec, hasShellExec, urls, markers, hasCredLiteral, hasDynamicArgs, skillRefs := parseSkillBody(string(body))
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
			HasShellExec:           hasShellExec,
			ExternalURLs:           urls,
			InjectionMarkers:       markers,
			HasCredentialLiteral:   hasCredLiteral,
			HasDynamicArgs:         hasDynamicArgs,
			ReferencesSkills:       skillRefs,
			BundledFiles:           bundledFiles(manifest.RepoRoot, p),
			Body:                   string(body),
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
	// skillShellFenceRe matches an ordinary ```bash or ```sh fenced code block
	// (model-invocation time shell, distinct from the pre-model ```! exec fence).
	skillShellFenceRe = regexp.MustCompile("(?m)^```(?:bash|sh)(?:\\s|$)")
	// skillShellInlineRe matches subshell syntax and common shell-invocation
	// patterns in non-fenced body text.
	skillShellInlineRe = regexp.MustCompile(`\$\(|\bsubprocess\b|\bos\.system\b`)
	// skillCredentialLiteralRe matches hardcoded secret VALUES committed in the
	// SKILL.md body itself (not bundled scripts). Mirrors bundledSecretLiteralRe
	// for the body surface.
	skillCredentialLiteralRe = regexp.MustCompile(
		`AKIA[0-9A-Z]{16}` + // AWS access key id
			`|ghp_[0-9A-Za-z]{36}` + // GitHub personal access token (classic)
			`|github_pat_[0-9A-Za-z_]{82}` + // GitHub fine-grained PAT
			`|xox[baprs]-[0-9A-Za-z-]{10,72}` + // Slack token
			`|sk-[A-Za-z0-9]{40,}` + // OpenAI-style secret key
			`|AIza[0-9A-Za-z_-]{35}` + // Google API key
			`|-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`) // private-key block header
	// skillDynamicArgsRe matches $ARGUMENTS (the Claude Code user-text
	// placeholder) and common template-interpolation patterns.
	skillDynamicArgsRe = regexp.MustCompile(`\$ARGUMENTS|\{\{[^}]+\}\}|\$INPUT\b|\$USER_INPUT\b`)
	// skillChainRefRe matches references to other skill invocations in the body.
	// Group 1 captures the skill name from /skill <name> or skill: <name>.
	skillChainRefRe = regexp.MustCompile(`(?i)/skill\s+(\S+)`)
)

// zeroWidthChars are invisible code points used to smuggle injected instructions
// past human review: zero-width space, zero-width non-joiner, zero-width joiner,
// and the byte-order mark.
const zeroWidthChars = "\u200b\u200c\u200d\ufeff"

// hasUnicodeTagsBlock reports whether s contains a Unicode Tags-block code point
// (U+E0000\u2013U+E007F). Each ASCII character has an invisible Tags equivalent, so
// this block is the carrier for "ASCII smuggling": instructions rendered
// invisibly to a human reviewer but read verbatim by the model.
func hasUnicodeTagsBlock(s string) bool {
	for _, r := range s {
		if r >= 0xE0000 && r <= 0xE007F {
			return true
		}
	}
	return false
}

// hasBidiControl reports whether s contains a bidirectional-override control
// (U+202A\u2013U+202E or U+2066\u2013U+2069). These reorder displayed text without
// changing its logical order \u2014 the Trojan-Source technique for hiding the real
// content of a line from review.
func hasBidiControl(s string) bool {
	for _, r := range s {
		if (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069) {
			return true
		}
	}
	return false
}

// parseSkillBody extracts security-relevant facts from the markdown body of a
// SKILL.md (everything below the frontmatter). Output is deterministic: commands,
// URLs, and skill refs are returned in first-seen order with duplicates removed;
// markers are fixed symbolic tokens. See models.SkillDef for field semantics.
func parseSkillBody(body string) (dynExec []string, hasShellExec bool, urls []string, markers []string, hasCredLiteral, hasDynamicArgs bool, skillRefs []string) {
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
	hasShellExec = skillShellFenceRe.MatchString(body) || skillShellInlineRe.MatchString(body)
	urls = skillExternalURLRe.FindAllString(body, -1)
	if skillInjectionPhraseRe.MatchString(body) {
		markers = append(markers, "instruction-override-phrase")
	}
	if strings.ContainsAny(body, zeroWidthChars) {
		markers = append(markers, "zero-width-characters")
	}
	if hasUnicodeTagsBlock(body) {
		markers = append(markers, "unicode-tags-smuggling")
	}
	if hasBidiControl(body) {
		markers = append(markers, "bidi-control-characters")
	}
	if skillBase64BlobRe.MatchString(body) {
		markers = append(markers, "long-base64-blob")
	}
	hasCredLiteral = skillCredentialLiteralRe.MatchString(body)
	hasDynamicArgs = skillDynamicArgsRe.MatchString(body)
	for _, m := range skillChainRefRe.FindAllStringSubmatch(body, -1) {
		skillRefs = append(skillRefs, m[1])
	}
	return dedupeStrings(dynExec), hasShellExec, dedupeStrings(urls), markers, hasCredLiteral, hasDynamicArgs, dedupeStrings(skillRefs)
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

// maxBundledScriptScanBytes caps how much of a bundled script is read for
// content facts. Scripts are small; the cap bounds the read so a pathologically
// large file cannot blow up discovery.
const maxBundledScriptScanBytes = 1 << 20 // 1 MiB

var (
	// bundledCommentRe matches a shell/python `#` line comment (from the first
	// `#` to end of line). The "script does X" facts (egress / secret read) are
	// matched against comment-stripped content so a word like `curl` or
	// `credentials` mentioned only in a comment is not read as a real call.
	bundledCommentRe = regexp.MustCompile(`(?m)#.*$`)
	// bundledEgressRe matches a network-egress invocation in a bundled script
	// (curl / wget / nc / ...). Mirrors the dynamic-exec egress check.
	bundledEgressRe = regexp.MustCompile(`(?i)\b(?:curl|wget|nc|ncat|telnet|scp|sftp|rsync)\b`)
	// bundledSecretRe matches a credential/secret read in a bundled script.
	bundledSecretRe = regexp.MustCompile(`(?i)gh\s+auth|printenv|\$(?:AWS|GH|GITHUB|OPENAI|ANTHROPIC|HF|NPM|SLACK|GOOGLE|GCP|AZURE)[A-Z0-9_]*|\bid_rsa\b|\bcredentials\b|\.aws/|\.ssh/|\.netrc\b|access[_-]?key|secret[_-]?key|api[_-]?key`)
	// bundledSecretLiteralRe matches a hardcoded secret VALUE committed in a
	// bundled file: distinctive provider token prefixes and private-key headers,
	// chosen for near-zero false positives (format/context, not entropy). Unlike
	// bundledSecretRe — which flags a script that READS a secret — this flags a
	// credential checked into the skill itself.
	bundledSecretLiteralRe = regexp.MustCompile(
		`AKIA[0-9A-Z]{16}` + // AWS access key id
			`|ghp_[0-9A-Za-z]{36}` + // GitHub personal access token (classic)
			`|github_pat_[0-9A-Za-z_]{82}` + // GitHub fine-grained PAT
			`|xox[baprs]-[0-9A-Za-z-]{10,72}` + // Slack token
			`|sk-[A-Za-z0-9]{40,}` + // OpenAI-style secret key
			`|AIza[0-9A-Za-z_-]{35}` + // Google API key
			`|-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`) // private-key block header
)

// readCapped reads up to maxBytes from path. A read error yields no content, so
// the file is simply left un-scanned (no fact stamped) rather than failing the
// whole skill's discovery.
func readCapped(path string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxBytes))
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
		bf := models.BundledFile{Path: filepath.ToSlash(rel), Kind: classifyBundledFile(d.Name())}
		// Content-scan every non-binary bundled file: a skill can run its scripts
		// (egress / secret reads), and any text file can carry a committed secret
		// literal — both surfaces that scanning SKILL.md alone misses.
		if bf.Kind != "binary" {
			if content, rerr := readCapped(abs, maxBundledScriptScanBytes); rerr == nil {
				// A committed secret literal counts even inside a comment, so scan raw.
				bf.HasHardcodedSecret = bundledSecretLiteralRe.Match(content)
				if bf.Kind == "script" {
					// Egress / secret-read describe executed behavior, so strip `#`
					// line comments first: `# never curl secrets` is not a real call.
					code := bundledCommentRe.ReplaceAll(content, nil)
					bf.HasNetworkEgress = bundledEgressRe.Match(code)
					bf.ReadsSecrets = bundledSecretRe.Match(code)
				}
			}
		}
		out = append(out, bf)
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
