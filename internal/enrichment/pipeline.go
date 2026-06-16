package enrichment

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/trustabl/trustabl/internal/models"
)

func init() {
	log.SetFlags(0) // suppress timestamps; prefix/severity come from the message itself
}

// Pipeline orchestrates enrichment of a ScanResult.
type Pipeline struct {
	LLMProvider  string   // "anthropic" | "openai" | "google"
	LLMKey       string   // API key from llm.Load()
	LLMModel     string   // model name from llm.Load(), e.g. "claude-haiku-4-5"
	RepoRoot     string   // root of the scanned repo; source files are read relative to this
	RuleFilter   []string // if non-empty, only enrich findings whose RuleID is in this list
	Apply        bool     // write AI-generated replacements to disk
	OnlyEnriched bool     // drop findings that could not be enriched from output
	Diff         bool     // write unified diff of proposed replacements to stderr

	llm llmEnricher // injected in tests; nil = create real client from LLMKey/LLMModel
}

func (p *Pipeline) shouldEnrich(f models.Finding) bool {
	if f.FilePath == "" || f.StartLine <= 0 {
		return false
	}
	if len(p.RuleFilter) == 0 {
		return true
	}
	for _, id := range p.RuleFilter {
		if id == f.RuleID {
			return true
		}
	}
	return false
}

// Run enriches result and returns an EnrichmentResult. Findings that cannot be
// enriched (file unreadable, LLM error) are passed through with Enriched=false.
func (p *Pipeline) Run(ctx context.Context, result *models.ScanResult) (*models.EnrichmentResult, error) {
	client := p.llm
	if client == nil {
		if p.LLMKey == "" {
			return nil, fmt.Errorf("enrichment: no LLM key configured — run: trustabl llm key set")
		}
		var clientErr error
		client, clientErr = newLLMClient(ctx, p.LLMProvider, p.LLMKey, p.LLMModel)
		if clientErr != nil {
			return nil, clientErr
		}
	}

	// Collect indices of findings to enrich.
	toEnrich := make([]int, 0, len(result.Findings))
	for i, f := range result.Findings {
		if p.shouldEnrich(f) {
			toEnrich = append(toEnrich, i)
		}
	}
	log.Printf("enriching %d/%d findings (grouped by file)", len(toEnrich), len(result.Findings))

	enriched := make([]models.EnrichedFinding, len(result.Findings))
	for i, f := range result.Findings {
		enriched[i] = models.EnrichedFinding{Finding: f}
	}

	// Group eligible finding indices by file path.
	byFile := make(map[string][]int)
	for _, idx := range toEnrich {
		fp := result.Findings[idx].FilePath
		byFile[fp] = append(byFile[fp], idx)
	}

	type job struct {
		filePath string
		indices  []int
	}
	jobs := make(chan job, len(byFile))
	for fp, idxs := range byFile {
		jobs <- job{filePath: fp, indices: idxs}
	}
	close(jobs)

	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				findings := make([]models.Finding, len(j.indices))
				for k, idx := range j.indices {
					findings[k] = result.Findings[idx]
				}
				results := p.enrichFile(ctx, client, j.filePath, findings)
				mu.Lock()
				for k, idx := range j.indices {
					enriched[idx] = results[k]
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if p.Diff {
		for _, ef := range enriched {
			if ef.Diff != "" {
				fmt.Fprint(os.Stderr, ef.Diff)
			}
		}
	}

	out := enriched
	if p.OnlyEnriched {
		filtered := out[:0]
		for _, ef := range out {
			if ef.Enriched {
				filtered = append(filtered, ef)
			}
		}
		out = filtered
	}

	return &models.EnrichmentResult{
		ScanID:       result.ScanID,
		Repo:         result.Repo,
		RulesVersion: result.RulesVersion,
		Findings:     out,
		EnrichedAt:   time.Now().UnixMilli(),
	}, nil
}

func (p *Pipeline) enrichFile(ctx context.Context, client llmEnricher, filePath string, findings []models.Finding) []models.EnrichedFinding {
	out := make([]models.EnrichedFinding, len(findings))
	for i, f := range findings {
		out[i] = models.EnrichedFinding{Finding: f}
	}

	absPath := filepath.Join(p.RepoRoot, filePath)
	fileContent, err := os.ReadFile(absPath)
	if err != nil {
		log.Printf("warn: read %s: %v", filePath, err)
		return out
	}

	issues := make([]issueContext, len(findings))
	for i, f := range findings {
		issues[i] = issueContext{
			ruleID:      f.RuleID,
			title:       f.Title,
			severity:    string(f.Severity),
			ruleScope:   string(f.Scope),
			line:        f.StartLine,
			explanation: f.Explanation,
			fixTemplate: f.SuggestedFix,
			codeBlock:   extractScope(string(fileContent), f.StartLine, 5),
		}
	}

	results, enrichErr := client.enrichFile(ctx, filePath, issues)
	if enrichErr != nil {
		log.Printf("warn: llm enrich %s: %v", filePath, enrichErr)
		return out
	}

	if len(results) < len(issues) {
		log.Printf("warn: llm returned %d results for %d issues in %s", len(results), len(issues), filePath)
	}

	lines := strings.Split(string(fileContent), "\n")

	type indexedPatch struct {
		idx   int
		patch filePatch
	}
	var patches []indexedPatch

	for i := range out {
		if i >= len(results) {
			break
		}
		r := results[i]
		out[i].AIExplanation = r.Explanation
		out[i].AIFix = r.Fix
		out[i].LineStart = r.LineStart
		out[i].LineEnd = r.LineEnd
		out[i].Replacement = r.Replacement
		out[i].FalsePositive = r.FalsePositive
		out[i].CodeSnippet = issues[i].codeBlock
		out[i].Enriched = r.Explanation != ""

		if p.Diff && r.LineStart > 0 && r.LineEnd >= r.LineStart && r.Replacement != "" {
			out[i].Diff = unifiedDiff(filePath, lines, r.LineStart, r.LineEnd, r.Replacement)
		}

		if p.Apply && r.LineStart > 0 && r.LineEnd >= r.LineStart && r.Replacement != "" && !r.FalsePositive {
			// Content anchor: only apply when the file STILL holds the exact lines
			// the model echoed back. A mismatch means the line numbers no longer
			// point at the reviewed code (stale scan, edited file, or a
			// misaligned/hallucinated result), so writing there would clobber
			// unrelated lines — skip it instead.
			if patchAnchorMatches(lines, r.LineStart, r.LineEnd, r.Original) {
				patches = append(patches, indexedPatch{
					idx: i,
					patch: filePatch{
						lineStart:   r.LineStart,
						lineEnd:     r.LineEnd,
						replacement: r.Replacement,
					},
				})
			} else {
				log.Printf("warn: skipping --apply fix for %s lines %d-%d (%s): the file no longer matches what the model reviewed (changed since the scan?); left unchanged",
					filePath, r.LineStart, r.LineEnd, findings[i].RuleID)
			}
		}
	}

	if len(patches) > 0 {
		rawPatches := make([]filePatch, len(patches))
		for i, ip := range patches {
			rawPatches[i] = ip.patch
		}
		updated, applyErr := applyPatches(string(fileContent), rawPatches)
		if applyErr != nil {
			log.Printf("warn: apply patches to %s: %v", filePath, applyErr)
		} else {
			dest := filepath.Join(p.RepoRoot, filePath)
			// Write a recovery backup of the ORIGINAL before overwriting. If the
			// backup cannot be written, skip the apply rather than leave the user
			// with no way back from a bad LLM rewrite.
			if bakErr := os.WriteFile(dest+".trustabl.bak", fileContent, 0o644); bakErr != nil {
				log.Printf("warn: could not write backup %s.trustabl.bak (%v); skipping --apply to avoid an unrecoverable overwrite", dest, bakErr)
			} else if tmp, tmpErr := writeTmp(dest, []byte(updated)); tmpErr != nil {
				log.Printf("warn: write tmp for %s: %v", dest, tmpErr)
			} else if renameErr := os.Rename(tmp, dest); renameErr != nil {
				_ = os.Remove(tmp)
				log.Printf("warn: rename %s: %v", dest, renameErr)
			} else {
				log.Printf("applied %d fix(es) to %s (backup: %s.trustabl.bak)", len(patches), filePath, filePath)
				for _, ip := range patches {
					out[ip.idx].Applied = true
				}
			}
		}
	}

	return out
}

// patchAnchorMatches is the --apply content anchor: it reports whether the
// current file's lines [lineStart,lineEnd] (1-based) exactly equal the `original`
// the model echoed. A mismatch — or a missing echo — means the patch can no
// longer be applied safely, so the caller must skip it rather than overwrite by
// line number alone.
func patchAnchorMatches(lines []string, lineStart, lineEnd int, original string) bool {
	if original == "" {
		return false // no verbatim echo → cannot verify → fail safe
	}
	if lineStart < 1 || lineEnd > len(lines) || lineStart > lineEnd {
		return false
	}
	return strings.Join(lines[lineStart-1:lineEnd], "\n") == original
}

type filePatch struct {
	lineStart   int
	lineEnd     int
	replacement string
}

func applyPatches(content string, patches []filePatch) (string, error) {
	lines := strings.Split(content, "\n")

	sort.Slice(patches, func(i, j int) bool {
		return patches[i].lineStart > patches[j].lineStart
	})

	// Detect overlapping ranges (after descending sort, patches[i].lineStart >= patches[i+1].lineStart).
	for i := 0; i+1 < len(patches); i++ {
		if patches[i+1].lineEnd >= patches[i].lineStart {
			return "", fmt.Errorf("overlapping patch ranges: [%d-%d] and [%d-%d]",
				patches[i+1].lineStart, patches[i+1].lineEnd,
				patches[i].lineStart, patches[i].lineEnd)
		}
	}

	for _, patch := range patches {
		start := patch.lineStart - 1
		end := patch.lineEnd - 1
		if start < 0 || end >= len(lines) || start > end {
			return "", fmt.Errorf("line range %d-%d out of bounds (file has %d lines)",
				patch.lineStart, patch.lineEnd, len(lines))
		}
		replacement := strings.Split(patch.replacement, "\n")
		lines = append(lines[:start], append(replacement, lines[end+1:]...)...)
	}

	return strings.Join(lines, "\n"), nil
}

// unifiedDiff returns a unified-diff string comparing original lines [lineStart-1:lineEnd]
// with replacement. lineStart and lineEnd are 1-based. Returns "" if content is identical
// or if the line range is out of bounds.
func unifiedDiff(filePath string, allLines []string, lineStart, lineEnd int, replacement string) string {
	if lineStart < 1 || lineEnd > len(allLines) || lineStart > lineEnd {
		return ""
	}
	origSlice := allLines[lineStart-1 : lineEnd]
	if strings.Join(origSlice, "\n") == replacement {
		return ""
	}

	replLines := strings.Split(replacement, "\n")
	ctxBefore := allLines[max(0, lineStart-1-2) : lineStart-1]
	ctxAfter := allLines[lineEnd:min(len(allLines), lineEnd+2)]

	hunkStart := lineStart - len(ctxBefore)
	origCount := len(ctxBefore) + len(origSlice) + len(ctxAfter)
	newCount := len(ctxBefore) + len(replLines) + len(ctxAfter)

	var sb strings.Builder
	fmt.Fprintf(&sb, "--- a/%s\n", filePath)
	fmt.Fprintf(&sb, "+++ b/%s\n", filePath)
	fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", hunkStart, origCount, hunkStart, newCount)
	for _, l := range ctxBefore {
		fmt.Fprintf(&sb, " %s\n", l)
	}
	for _, l := range origSlice {
		fmt.Fprintf(&sb, "-%s\n", l)
	}
	for _, l := range replLines {
		fmt.Fprintf(&sb, "+%s\n", l)
	}
	for _, l := range ctxAfter {
		fmt.Fprintf(&sb, " %s\n", l)
	}
	return sb.String()
}

// writeTmp writes data to a temp file in the same directory as dest and returns its path.
func writeTmp(dest string, data []byte) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(dest), ".trustabl-enrich-*.tmp")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
