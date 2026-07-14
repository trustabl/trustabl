package scanner

import (
	"fmt"

	"github.com/trustabl/trustabl/internal/models"
)

// secretFindings synthesizes one Finding per SecretMatch so secrets flow
// through the normal findings pipeline — exit codes, SARIF, and the report.
// This is the --secret-scan analog of vulnFindings, not a YAML rule.
func secretFindings(matches []models.SecretMatch) []models.Finding {
	out := make([]models.Finding, 0, len(matches))
	for _, m := range matches {
		var sev models.Severity
		var title, expl, fix string
		switch m.RuleID {
		case "SECRET-LIT-001":
			sev = models.SeverityHigh
			title = "Hardcoded credential literal"
			expl = fmt.Sprintf(
				"%s (line %d) contains a hardcoded credential literal — a "+
					"provider-format token committed directly in source. "+
					"If this file is pushed to a remote repository the credential "+
					"is exposed to anyone with read access and will likely be "+
					"harvested by secret-scanning bots within minutes.",
				m.File, m.Line)
			fix = "Revoke and rotate the credential immediately. " +
				"Store secrets in environment variables, a secrets manager " +
				"(e.g. AWS Secrets Manager, HashiCorp Vault, GitHub Actions secrets), " +
				"or a .env file excluded from version control, and reference them " +
				"at runtime instead of hardcoding them."
		case "SECRET-ENV-001":
			sev = models.SeverityMedium
			title = "Script reads credentials from environment"
			expl = fmt.Sprintf(
				"%s (line %d) reads or prints a credential environment variable "+
					"(e.g. $ANTHROPIC_API_KEY, $AWS_SECRET_ACCESS_KEY, gh auth). "+
					"If this script is invoked by an agent it may leak credentials "+
					"to the model's context window, tool output, or log files.",
				m.File, m.Line)
			fix = "Avoid passing raw credentials through scripts invoked by agents. " +
				"Prefer SDKs that inject credentials transparently, " +
				"scope the credential to the minimum required permissions, " +
				"and ensure script output is not returned verbatim to the model."
		default:
			sev = models.SeverityMedium
			title = "Potential secret detected"
			expl = fmt.Sprintf("%s (line %d): pattern matched by %s.", m.File, m.Line, m.RuleID)
			fix = "Review the flagged line and rotate the credential if it is real."
		}
		out = append(out, models.Finding{
			RuleID:       m.RuleID,
			Severity:     sev,
			FilePath:     m.File,
			StartLine:    m.Line,
			EndLine:      m.Line,
			Title:        title,
			Explanation:  expl,
			SuggestedFix: fix,
			Confidence:   0.85,
		})
	}
	return out
}
