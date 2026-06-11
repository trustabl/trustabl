package acac

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/trustabl/trustabl/internal/models"
)

// OpenShell policy export (spec §6, Stage 3). The generator emits a full,
// self-contained policy — static blocks always included — suitable for
// sandbox creation and `openshell policy set`. Lifecycle caveat the docs must
// carry: filesystem_policy and process are locked at sandbox creation; only
// network_policies hot-reload.
//
// Conservative emission defaults: endpoint access is read-only with a
// suggested-confirm marker (method usage is not provable), binaries carry an
// interpreter-path guess with a review marker, and literal private-range /
// loopback / link-local hosts are NEVER emitted as endpoints — they surface
// as review notes instead. Dynamic-URL tools produce review-note stubs, never
// guessed policies.

// Baseline hardening defaults, mirroring OpenShell's own
// restrictive_default_policy() (crates/openshell-policy/src/lib.rs @ v0.0.59):
// the same 7 read-only and 3 read-write roots the runtime ships as its safe
// default, so a generated policy is never thinner than OpenShell's baseline.
var (
	openShellReadOnly  = []string{"/usr", "/lib", "/proc", "/dev/urandom", "/app", "/etc", "/var/log"}
	openShellReadWrite = []string{"/sandbox", "/tmp", "/dev/null"}
)

const (
	openShellUser  = "sandbox"
	openShellGroup = "sandbox"

	// OpenShell's documented validation constraints, mirrored at generate
	// time so an invalid policy is never written.
	openShellMaxPathLen   = 4096
	openShellMaxPathCount = 256
)

// OpenShellPolicy is the policy document model, mirroring the verified
// schema: version 1, filesystem_policy, landlock, process, network_policies.
type OpenShellPolicy struct {
	ReadOnly   []string // filesystem_policy.read_only
	ReadWrite  []string // filesystem_policy.read_write (baseline ∪ captured absolute write paths)
	RunAsUser  string   // process.run_as_user — never root (validated)
	RunAsGroup string   // process.run_as_group — never root (validated)
	Network    []OpenShellNetworkPolicy

	// ReviewNotes are emitted as review-marker comments above the
	// network_policies block: dynamic-URL tools, private/loopback hosts,
	// relative write paths — everything detected but not safely derivable.
	ReviewNotes []string
}

// OpenShellNetworkPolicy is one network_policies entry: a policy for one
// outbound tool with at least one captured static host.
type OpenShellNetworkPolicy struct {
	Key       string // YAML key (identifier-sanitized tool name)
	Name      string // policy name (slug)
	Endpoints []OpenShellEndpoint
	Binaries  []string // interpreter-path guesses (review marker)
}

// OpenShellEndpoint is one allowed endpoint, conservative by default.
type OpenShellEndpoint struct {
	Host string
	Port int
}

// BuildOpenShellPolicy derives a policy from the selected agent's tool graph
// — the same selection unit as the manifest. Deterministic: tools processed
// in sorted order, all lists sorted and deduped.
func BuildOpenShellPolicy(result models.ScanResult, agent models.AgentDef) OpenShellPolicy {
	p := OpenShellPolicy{
		ReadOnly:   append([]string{}, openShellReadOnly...),
		ReadWrite:  append([]string{}, openShellReadWrite...),
		RunAsUser:  openShellUser,
		RunAsGroup: openShellGroup,
	}

	// The graph's resolved tools, sorted by (Name, FilePath).
	var tools []models.ToolDef
	seen := map[string]bool{}
	for _, ref := range agent.ToolRefs {
		if ref.Resolved == nil {
			continue
		}
		key := ref.Resolved.FilePath + "\x00" + ref.Resolved.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		tools = append(tools, *ref.Resolved)
	}
	sort.Slice(tools, func(i, j int) bool {
		if tools[i].Name != tools[j].Name {
			return tools[i].Name < tools[j].Name
		}
		return tools[i].FilePath < tools[j].FilePath
	})

	writeSet := map[string]bool{}
	for _, rw := range p.ReadWrite {
		writeSet[rw] = true
	}
	aliases := newAliasSet()
	for _, t := range tools {
		// Filesystem: captured absolute write paths extend read_write; a
		// relative literal cannot satisfy OpenShell's absolute-path rule, so
		// it becomes a review note instead of a guess.
		for _, wp := range t.FSWritePaths {
			if strings.HasPrefix(wp, "/") {
				writeSet[wp] = true
			} else {
				p.ReviewNotes = append(p.ReviewNotes,
					fmt.Sprintf("tool %s writes to relative path %q — map it under the sandbox workdir and add it to read_write", t.Name, wp))
			}
		}

		// Network: one policy per tool with at least one emittable host.
		var endpoints []OpenShellEndpoint
		for _, hp := range t.HTTPHosts {
			host, portStr, err := net.SplitHostPort(hp)
			if err != nil {
				host, portStr = hp, "443"
			}
			port, err := strconv.Atoi(portStr)
			if err != nil {
				continue
			}
			if reason, blocked := hostBlockedReason(host); blocked {
				p.ReviewNotes = append(p.ReviewNotes,
					fmt.Sprintf("tool %s targets %s host %s — not emitted; add an explicit allowed_ips entry if intended", t.Name, reason, hp))
				continue
			}
			endpoints = append(endpoints, OpenShellEndpoint{Host: host, Port: port})
		}
		if t.Facts["dynamic_url"] == "true" || (t.Facts["http_call"] == "true" && len(endpoints) == 0 && len(t.HTTPHosts) == 0) {
			p.ReviewNotes = append(p.ReviewNotes,
				fmt.Sprintf("tool %s makes HTTP calls whose URL is not a static literal — define its network policy manually", t.Name))
		}
		if len(endpoints) == 0 {
			continue
		}
		np := OpenShellNetworkPolicy{
			Key:       aliases.claim(t.Name),
			Name:      dnsName(t.Name),
			Endpoints: endpoints,
			Binaries:  interpreterGuess(t.Language),
		}
		p.Network = append(p.Network, np)
	}

	p.ReadWrite = setToSortedKeys(writeSet)
	sort.Strings(p.ReviewNotes)
	return p
}

// hostBlockedReason lexically classifies hosts that must never be emitted as
// endpoints: loopback, link-local, and RFC 1918 private ranges. Lexical only
// — classification never resolves DNS (determinism contract); a private host
// hiding behind a public-looking name is the documented residual gap.
func hostBlockedReason(host string) (string, bool) {
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return "loopback", true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return "", false
	}
	switch {
	case ip.IsLoopback():
		return "loopback", true
	case ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast():
		return "link-local", true
	case ip.IsPrivate():
		return "private-range", true
	}
	return "", false
}

// dnsName renders a tool name as a DNS-label-style policy name (the spec's
// example maps search_web → search-web): lowercase, [a-z0-9-] only,
// everything else collapses to single dashes, trimmed at both ends.
func dnsName(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "tool"
	}
	return s
}

// interpreterGuess maps a tool's language to the conventional interpreter
// path inside the sandbox. A guess by design — emitted with a review marker.
func interpreterGuess(lang models.Language) []string {
	switch lang {
	case models.LanguagePython:
		return []string{"/usr/bin/python3"}
	case models.LanguageTypeScript, models.LanguageJavaScript:
		return []string{"/usr/bin/node"}
	}
	return nil
}

// setToSortedKeys converts a string set to a sorted slice (always non-nil for
// the policy's baseline lists).
func setToSortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// ValidateOpenShellPolicy mirrors OpenShell's documented load-time rules as
// generate-time hard errors, so an invalid policy is never written:
// absolute paths only, no "..", no overly-broad roots, path length and count
// caps, non-root user/group, wildcards only as a leading first-label "*.",
// and no loopback/link-local/private host in any emitted field.
func ValidateOpenShellPolicy(p OpenShellPolicy) error {
	pathCount := 0
	checkPath := func(path, where string) error {
		pathCount++
		if !strings.HasPrefix(path, "/") {
			return fmt.Errorf("openshell policy: %s path %q is not absolute", where, path)
		}
		if path == "/" || overlyBroadRoot(path, where) {
			return fmt.Errorf("openshell policy: %s path %q is an overly broad root", where, path)
		}
		for _, seg := range strings.Split(path, "/") {
			if seg == ".." {
				return fmt.Errorf("openshell policy: %s path %q contains a '..' segment", where, path)
			}
		}
		if len(path) > openShellMaxPathLen {
			return fmt.Errorf("openshell policy: %s path exceeds %d characters", where, openShellMaxPathLen)
		}
		return nil
	}
	for _, ro := range p.ReadOnly {
		if err := checkPath(ro, "read_only"); err != nil {
			return err
		}
	}
	for _, rw := range p.ReadWrite {
		if err := checkPath(rw, "read_write"); err != nil {
			return err
		}
	}
	switch {
	case p.RunAsUser == "" || p.RunAsGroup == "":
		return fmt.Errorf("openshell policy: process user/group must be set")
	case p.RunAsUser == "root" || p.RunAsUser == "0" || p.RunAsGroup == "root" || p.RunAsGroup == "0":
		return fmt.Errorf("openshell policy: process must not run as root")
	}
	for _, np := range p.Network {
		for _, ep := range np.Endpoints {
			if i := strings.IndexByte(ep.Host, '*'); i >= 0 {
				if i != 0 || !strings.HasPrefix(ep.Host, "*.") || strings.Contains(ep.Host[2:], "*") {
					return fmt.Errorf("openshell policy: endpoint host %q — wildcards are allowed only as a leading first-label \"*.\"", ep.Host)
				}
			}
			if reason, blocked := hostBlockedReason(ep.Host); blocked {
				return fmt.Errorf("openshell policy: endpoint host %q is %s and must not be emitted", ep.Host, reason)
			}
			if ep.Port < 1 || ep.Port > 65535 {
				return fmt.Errorf("openshell policy: endpoint %s has out-of-range port %d", ep.Host, ep.Port)
			}
		}
		for _, b := range np.Binaries {
			if err := checkPath(b, "binaries"); err != nil {
				return err
			}
		}
	}
	if pathCount > openShellMaxPathCount {
		return fmt.Errorf("openshell policy: %d paths exceeds the %d-path cap", pathCount, openShellMaxPathCount)
	}
	return nil
}

// overlyBroadRoot rejects write access to whole system roots. Read-only
// system roots are the baseline hardening defaults, so the check applies to
// the writable list and binaries only.
func overlyBroadRoot(path, where string) bool {
	if where == "read_only" {
		return false
	}
	switch path {
	case "/etc", "/usr", "/bin", "/sbin", "/lib", "/lib64", "/var", "/home", "/root", "/boot", "/proc", "/sys", "/dev":
		return true
	}
	return false
}
