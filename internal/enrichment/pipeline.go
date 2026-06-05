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
	LLMKey       string   // Anthropic API key from llm.Load()
	LLMModel     string   // model name from llm.Load(), e.g. "claude-haiku-4-5"
	RepoRoot     string   // root of the scanned repo; source files are read relative to this
	RuleFilter   []string // if non-empty, only enrich findings whose RuleID is in this list
	Apply        bool     // write AI-generated replacements to disk
	OnlyEnriched bool     // drop findings that could not be enriched from output

	llm llmEnricher // injected in tests; nil = create real client from LLMKey/LLMModel
}

func (p *Pipeline) shouldEnrich(f models.Finding) bool {
	if f.FilePath == "" || f.Line <= 0 {
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
		client = newLLMClient(p.LLMKey, p.LLMModel)
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
			line:        f.Line,
			explanation: f.Explanation,
			fixTemplate: f.SuggestedFix,
			codeBlock:   extractScope(string(fileContent), f.Line, 5),
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

		if p.Apply && r.LineStart > 0 && r.LineEnd >= r.LineStart && r.Replacement != "" && !r.FalsePositive {
			patches = append(patches, indexedPatch{
				idx: i,
				patch: filePatch{
					lineStart:   r.LineStart,
					lineEnd:     r.LineEnd,
					replacement: r.Replacement,
				},
			})
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
			tmp, tmpErr := writeTmp(dest, []byte(updated))
			if tmpErr != nil {
				log.Printf("warn: write tmp for %s: %v", dest, tmpErr)
			} else if renameErr := os.Rename(tmp, dest); renameErr != nil {
				_ = os.Remove(tmp)
				log.Printf("warn: rename %s: %v", dest, renameErr)
			} else {
				log.Printf("applied %d fix(es) to %s", len(patches), filePath)
				for _, ip := range patches {
					out[ip.idx].Applied = true
				}
			}
		}
	}

	return out
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
