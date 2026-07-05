package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
)

// DetectCIProvider returns which CI system is running, detected by env var
// presence only — no values are read. Returns "" when not in CI.
func DetectCIProvider() string {
	switch {
	case os.Getenv("GITHUB_ACTIONS") != "":
		return "github_actions"
	case os.Getenv("GITLAB_CI") != "":
		return "gitlab_ci"
	case os.Getenv("CIRCLECI") != "":
		return "circleci"
	case os.Getenv("JENKINS_URL") != "":
		return "jenkins"
	case os.Getenv("CI") != "":
		return "unknown"
	default:
		return ""
	}
}

// repoHashSalt is mixed into the hash to prevent trivial rainbow-table
// reversal. The value must never change — changing it breaks deduplication
// across all previously collected hashes.
const repoHashSalt = "trustabl-telemetry-v1"

// RepoIDHash returns a one-way hash of the current repo's CI identifier,
// suitable for counting unique repos without revealing their identity.
// Returns "" when no known CI repo env var is set.
func RepoIDHash() string {
	var repo string
	switch {
	case os.Getenv("GITHUB_REPOSITORY") != "":
		repo = os.Getenv("GITHUB_REPOSITORY")
	case os.Getenv("CI_PROJECT_PATH") != "": // GitLab
		repo = os.Getenv("CI_PROJECT_PATH")
	case os.Getenv("CIRCLE_PROJECT_REPONAME") != "": // CircleCI
		repo = os.Getenv("CIRCLE_PROJECT_REPONAME")
	default:
		return ""
	}
	h := sha256.Sum256([]byte(repoHashSalt + ":" + repo))
	return hex.EncodeToString(h[:16]) // 32 hex chars — enough for dedup
}
