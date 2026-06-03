package sarif

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/trustabl/trustabl/internal/models"
)

// levelForSeverity maps Trustabl's 5-bucket severity to SARIF's 4-bucket level.
// Mapping locked in design doc D3. critical/high → error; medium → warning;
// low/info → note.
func levelForSeverity(s models.Severity) string {
	switch s {
	case models.SeverityCritical, models.SeverityHigh:
		return "error"
	case models.SeverityMedium:
		return "warning"
	default: // low, info, unknown
		return "note"
	}
}

// securitySeverityForSeverity maps to the GitHub "security-severity" 0–10
// string used to drive the Critical/High/Medium/Low badge bucketing. Cutoffs
// in the GitHub UI: ≥9 critical, ≥7 high, ≥4 medium, <4 low. Mapping locked
// in design doc D3.
func securitySeverityForSeverity(s models.Severity) string {
	switch s {
	case models.SeverityCritical:
		return "9.0"
	case models.SeverityHigh:
		return "7.5"
	case models.SeverityMedium:
		return "5.5"
	case models.SeverityLow:
		return "3.0"
	default: // info, unknown
		return "0.5"
	}
}

// tagsForFinding builds the rule-descriptor tag set: category, scope (parsed
// from the rule ID prefix when applicable), and language (derived from the
// finding's file extension, since Trustabl now does first-class TypeScript /
// JavaScript discovery alongside Python).
func tagsForFinding(f models.Finding) []string {
	tags := []string{}
	if f.Category != "" {
		tags = append(tags, string(f.Category))
	}
	// Trustabl's rule IDs encode scope implicitly:
	//   CSDK-0xx / OAI-0xx → tool scope
	//   CSDK-1xx / OAI-1xx → agent scope
	//   OAI-2xx           → repo scope
	//   META-xxx          → no scope tag
	if scope := scopeFromRuleID(f.RuleID); scope != "" {
		tags = append(tags, scope)
	}
	tags = append(tags, languageTagForPath(f.FilePath))
	return tags
}

// languageTagForPath maps a finding's file path to a language tag for SARIF.
// Falls back to "python" when the path has no recognized source extension
// (e.g. repo-scope findings with no file), preserving prior behavior.
func languageTagForPath(path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".ts"), strings.HasSuffix(lower, ".tsx"),
		strings.HasSuffix(lower, ".mts"), strings.HasSuffix(lower, ".cts"):
		return "typescript"
	case strings.HasSuffix(lower, ".js"), strings.HasSuffix(lower, ".jsx"),
		strings.HasSuffix(lower, ".mjs"):
		return "javascript"
	default:
		return "python"
	}
}

// scopeFromRuleID returns the rule's scope tag based on its numeric prefix, or
// "" for META rules (no scope).
func scopeFromRuleID(id string) string {
	// id format: "<PREFIX>-<NNN>" where PREFIX is CSDK/OAI/META.
	// Scope buckets: 0xx tool, 1xx agent, 2xx repo.
	dash := -1
	for i := 0; i < len(id); i++ {
		if id[i] == '-' {
			dash = i
			break
		}
	}
	if dash < 0 || dash+1 >= len(id) {
		return ""
	}
	prefix := id[:dash]
	if prefix == "META" {
		return ""
	}
	first := id[dash+1]
	switch first {
	case '0':
		return "tool"
	case '1':
		return "agent"
	case '2':
		return "repo"
	}
	return ""
}

// ruleFromFinding builds a SARIF reportingDescriptor (rule catalog entry) from
// the first Finding emitted for a given rule. Title/Explanation/Fix are
// rule-stable; severity is rule-stable; confidence is rule-stable today (may
// vary per-finding once value-aware predicates land — the descriptor will then
// reflect the rule's default and result.rank carries the per-finding value).
func ruleFromFinding(f models.Finding) ReportingDescriptor {
	rd := ReportingDescriptor{
		ID:               f.RuleID,
		ShortDescription: &Message{Text: f.Title},
		FullDescription:  &Message{Text: f.Explanation},
		DefaultConfiguration: &ReportingConfiguration{
			Level: levelForSeverity(f.Severity),
		},
		Properties: map[string]any{
			"security-severity": securitySeverityForSeverity(f.Severity),
			"confidence":        f.Confidence,
			"tags":              tagsForFinding(f),
		},
	}
	// Omit help entirely when there's no fix text — an empty help.text is noise
	// for SARIF viewers. Mirrors the guard on result.Fixes.
	if f.SuggestedFix != "" {
		rd.Help = &Message{Text: f.SuggestedFix}
	}
	return rd
}

// resultFromFinding builds a SARIF Result from a Finding. ruleIndex points at
// the entry for f.RuleID in tool.driver.rules (or nil if unknown — defensive,
// shouldn't happen in normal flow). useBase controls whether the location
// references the REPO_ROOT uriBaseId: only when buildRun also emits the matching
// originalUriBaseIds entry (local scans), so the SARIF is never internally
// inconsistent with a dangling base reference.
func resultFromFinding(f models.Finding, ruleIndex *int, useBase bool) Result {
	r := Result{
		RuleID:    f.RuleID,
		RuleIndex: ruleIndex,
		Message:   Message{Text: f.Explanation},
		Properties: map[string]any{
			"confidence": f.Confidence,
		},
		PartialFingerprints: map[string]string{
			"primaryLocationLineHash": fingerprintFor(f),
		},
	}
	rank := f.Confidence * 100
	r.Rank = &rank

	if f.SuggestedFix != "" {
		r.Fixes = []Fix{{Description: Message{Text: f.SuggestedFix}}}
	}

	// Locations: physical when we have a file; logical when we have a tool name.
	if f.FilePath != "" {
		al := ArtifactLocation{URI: f.FilePath}
		if useBase {
			al.URIBaseID = "REPO_ROOT"
		}
		phys := &PhysicalLocation{ArtifactLocation: al}
		if f.Line > 0 {
			phys.Region = &Region{StartLine: f.Line}
		}
		loc := Location{PhysicalLocation: phys}
		if f.ToolName != "" {
			loc.LogicalLocations = []LogicalLocation{{Name: f.ToolName, Kind: "function"}}
		}
		r.Locations = []Location{loc}
	}

	// Kind: "informational" for META results and repo-scoped rule findings
	// (rule findings without a file location). Default (fail) for regular
	// located rule findings — emit empty so SARIF's default applies.
	if isInformational(f) {
		r.Kind = "informational"
	}

	return r
}

// isInformational returns true for findings that should carry SARIF
// kind="informational". Covers all META results that reach this builder
// (META-001/004 don't reach it — they're notifications) plus repo-scoped rule
// findings (which findingFromRule emits with empty FilePath/Line).
func isInformational(f models.Finding) bool {
	if len(f.RuleID) >= 4 && f.RuleID[:4] == "META" {
		return true
	}
	if f.FilePath == "" {
		return true
	}
	return false
}

// notificationFromFinding builds a SARIF Notification for META-001 / META-004.
// ruleIndex is the rule's position in tool.driver.rules; the notification
// references it via descriptor.index for consumer-side rule lookup.
func notificationFromFinding(f models.Finding, ruleIndex int) Notification {
	return Notification{
		Level:      "note",
		Message:    Message{Text: f.Explanation},
		Descriptor: &ReportingDescriptorReference{Index: ruleIndex},
		Properties: map[string]any{
			"rule_id": f.RuleID,
		},
	}
}

// fingerprintFor returns a stable hex SHA-256 of (RuleID|FilePath|ToolName).
// Identifies the same logical finding across scans even when line numbers
// shift, which is what GitHub Code Scanning uses to deduplicate alerts.
func fingerprintFor(f models.Finding) string {
	h := sha256.New()
	h.Write([]byte(f.RuleID))
	h.Write([]byte{'|'})
	h.Write([]byte(f.FilePath))
	h.Write([]byte{'|'})
	h.Write([]byte(f.ToolName))
	return hex.EncodeToString(h.Sum(nil))
}

// Render serializes a Trustabl ScanResult into a SARIF 2.1.0 JSON document
// (pretty-printed, trailing newline). toolVersion is reported as
// tool.driver.version/semanticVersion — the CLI passes its own version literal
// so the SARIF document never disagrees with `trustabl version`. The mapping
// rules are locked in .superpowers/specs/2026-05-24-sarif-output-design.md.
func Render(sr models.ScanResult, toolVersion string) []byte {
	log := Log{
		Version: "2.1.0",
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Runs:    []Run{buildRun(sr, toolVersion)},
	}
	out, _ := json.MarshalIndent(log, "", "  ")
	return append(out, '\n')
}

// sortFindings orders findings by (RuleID, FilePath, Line) so SARIF output is
// byte-stable regardless of the order findings are supplied in.
func sortFindings(fs []models.Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].RuleID != fs[j].RuleID {
			return fs[i].RuleID < fs[j].RuleID
		}
		if fs[i].FilePath != fs[j].FilePath {
			return fs[i].FilePath < fs[j].FilePath
		}
		return fs[i].Line < fs[j].Line
	})
}

// buildRun assembles the single run that Trustabl emits.
func buildRun(sr models.ScanResult, toolVersion string) Run {
	// Partition findings: META-001/004 → notifications; everything else → results.
	var resultFindings []models.Finding
	var notifyFindings []models.Finding
	for _, f := range sr.Findings {
		if f.RuleID == "META-001" || f.RuleID == "META-004" {
			notifyFindings = append(notifyFindings, f)
		} else {
			resultFindings = append(resultFindings, f)
		}
	}

	// Byte-stable SARIF is a hard contract; the renderer owns it rather than
	// inheriting whatever order sr.Findings arrived in, so a future change to
	// finding assembly upstream cannot perturb the emitted bytes.
	sortFindings(resultFindings)
	sortFindings(notifyFindings)

	// Build the referenced rule catalog from the first Finding seen for each
	// rule. Sort by ID for determinism.
	firstByRule := map[string]models.Finding{}
	for _, f := range sr.Findings {
		if _, ok := firstByRule[f.RuleID]; !ok {
			firstByRule[f.RuleID] = f
		}
	}
	ruleIDs := make([]string, 0, len(firstByRule))
	for id := range firstByRule {
		ruleIDs = append(ruleIDs, id)
	}
	sort.Strings(ruleIDs)
	rules := make([]ReportingDescriptor, 0, len(ruleIDs))
	indexByID := map[string]int{}
	for i, id := range ruleIDs {
		rules = append(rules, ruleFromFinding(firstByRule[id]))
		indexByID[id] = i
	}

	// A REPO_ROOT uriBaseId is only meaningful for a local scan (see the
	// originalUriBaseIds block below). Results must reference the base only when
	// it is actually emitted, or the SARIF is internally inconsistent.
	useBase := !sr.Manifest.IsRemote && sr.Manifest.RepoRoot != ""

	// Results.
	results := make([]Result, 0, len(resultFindings))
	for _, f := range resultFindings {
		idx := indexByID[f.RuleID]
		results = append(results, resultFromFinding(f, &idx, useBase))
	}

	// Notifications.
	notifications := make([]Notification, 0, len(notifyFindings)+1)
	for _, f := range notifyFindings {
		notifications = append(notifications, notificationFromFinding(f, indexByID[f.RuleID]))
	}
	// Surface incomplete parse coverage as a warning notification. We do NOT
	// flip ExecutionSuccessful to false: per the SARIF spec that means the run
	// did not complete (it crashed/errored), whereas here the run finished and
	// produced results — some inputs just could not be analyzed. The warning is
	// the honest signal so a consumer never reads a partial scan as a clean,
	// complete one. Appended last; the META notifications above are already in
	// deterministic order, and skipped_files is pre-sorted.
	if sr.Coverage.FilesSkipped > 0 {
		notifications = append(notifications, Notification{
			Level: "warning",
			Message: Message{Text: fmt.Sprintf(
				"%d of %d source files could not be parsed and were skipped; findings may be incomplete",
				sr.Coverage.FilesSkipped, sr.Coverage.FilesParsed+sr.Coverage.FilesSkipped)},
			Properties: map[string]any{
				"files_skipped": sr.Coverage.FilesSkipped,
				"files_parsed":  sr.Coverage.FilesParsed,
				"skipped_files": sr.Coverage.SkippedFiles,
			},
		})
	}

	run := Run{
		Tool: Tool{Driver: ToolComponent{
			Name:            "trustabl",
			FullName:        "Trustabl — static analyzer for agent reliability",
			InformationURI:  "https://github.com/trustabl/trustabl",
			Version:         toolVersion,
			SemanticVersion: toolVersion,
			Rules:           rules,
			Properties: map[string]any{
				"rules_source":     sr.RulesSource,
				"rules_version":    sr.RulesVersion,
				"rules_from_cache": sr.RulesFromCache,
			},
		}},
		AutomationDetails: &AutomationDetails{ID: sr.ScanID},
		Invocations: []Invocation{{
			// True: the run completed and produced results. Incomplete *coverage*
			// (skipped files) is signalled via a warning notification above, not
			// by claiming the run failed. See the coverage-warning block.
			ExecutionSuccessful:        true,
			ToolExecutionNotifications: notifications,
		}},
		Results: results,
	}
	// Only emit a REPO_ROOT base for LOCAL scans. For a remote scan the repo
	// root is a throwaway temp clone dir (trustabl-clone-*) that means nothing
	// to a SARIF consumer and would leak the local filesystem layout into the
	// report; GitHub code scanning resolves results by their repo-relative URI +
	// versionControlProvenance, not by uriBaseId. See review finding H7.
	if useBase {
		run.OriginalUriBaseIds = map[string]ArtifactLocation{
			"REPO_ROOT": {URI: repoRootURI(sr.Manifest.RepoRoot)},
		}
	}
	if sr.Manifest.IsRemote && sr.Manifest.RemoteURL != "" {
		run.VersionControlProvenance = []VersionControlProvenance{
			{RepositoryURI: sr.Manifest.RemoteURL},
		}
	}
	return run
}

// repoRootURI normalizes the repo root into a file:// URI ending with a slash,
// suitable for use as a SARIF uriBaseId base. The trailing slash matters for
// SARIF's resolution rules (uriBase + relative uri = full uri). The path is
// percent-encoded so a root containing spaces or non-ASCII characters produces
// a valid URI a SARIF validator will accept.
func repoRootURI(root string) string {
	root = strings.ReplaceAll(root, "\\", "/")
	if !strings.HasSuffix(root, "/") {
		root += "/"
	}
	// Treat absolute-looking roots as file URIs; otherwise leave as-is.
	if len(root) > 1 && (root[0] == '/' || (len(root) > 2 && root[1] == ':')) {
		// url.URL.String() percent-encodes the path while leaving "/" separators
		// intact. A leading "/" is required so a Windows drive path ("C:/...")
		// becomes file:///C:/... and a POSIX path ("/tmp/...") stays file:///tmp/...
		p := root
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		u := url.URL{Scheme: "file", Path: p}
		return u.String()
	}
	return root
}
