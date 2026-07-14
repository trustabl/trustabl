package analysis

import (
	"bytes"
	"io/fs"
	"path/filepath"
	"sort"

	"github.com/trustabl/trustabl/internal/models"
)

// DiscoverSecrets walks the entire repository for hardcoded secret literals and
// credential-reading scripts, returning one SecretMatch per detected occurrence.
// It is the opt-in --secret-scan layer: it runs repo-wide (all text files) so
// it catches secrets committed outside the .claude/ tree — in tool implementations,
// CI helpers, test fixtures, or any other source file.
//
// Two rule IDs are emitted:
//   - SECRET-LIT-001: a hardcoded credential literal committed in source (AWS key,
//     GitHub PAT, Slack token, OpenAI key, Google API key, private-key block).
//   - SECRET-ENV-001: a script that reads a credential from the environment at
//     runtime (e.g. $ANTHROPIC_API_KEY, gh auth, printenv SECRET).
//
// No raw secret value is recorded. Only file, 1-indexed line, and rule ID.
func DiscoverSecrets(repoRoot string) []models.SecretMatch {
	var out []models.SecretMatch
	_ = filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != repoRoot && skipForDeps(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if classifyBundledFile(d.Name()) == "binary" {
			return nil
		}
		content, rerr := readCapped(path, maxBundledScriptScanBytes)
		if rerr != nil || len(content) == 0 {
			return nil
		}
		rel, rerr := filepath.Rel(repoRoot, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		if loc := bundledSecretLiteralRe.FindIndex(content); loc != nil {
			out = append(out, models.SecretMatch{
				File:   rel,
				Line:   lineOf1(content, loc[0]),
				RuleID: "SECRET-LIT-001",
			})
		}
		if classifyBundledFile(d.Name()) == "script" {
			code := bundledCommentRe.ReplaceAll(content, nil)
			if loc := bundledSecretRe.FindIndex(code); loc != nil {
				out = append(out, models.SecretMatch{
					File:   rel,
					Line:   lineOf1(content, loc[0]),
					RuleID: "SECRET-ENV-001",
				})
			}
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.RuleID < b.RuleID
	})
	return out
}

// lineOf1 returns the 1-indexed line number of byte offset pos in content by
// counting newlines before it.
func lineOf1(content []byte, pos int) int {
	if pos > len(content) {
		pos = len(content)
	}
	return bytes.Count(content[:pos], []byte("\n")) + 1
}
