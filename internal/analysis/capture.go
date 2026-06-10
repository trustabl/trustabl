package analysis

import (
	"net"
	"net/url"
	"sort"
)

// hostFromURLLiteral parses a static URL literal and returns its canonical
// host:port. Only absolute http/https URLs with a hostname qualify — a
// relative URL ("/api/v1") or a non-HTTP scheme captures nothing. The
// scheme's default port is applied when the URL names none (https → 443,
// http → 80), so downstream consumers (x-trustabl surface facts, the
// OpenShell policy exporter) never have to re-derive a port from a scheme
// the inventory no longer carries. The host is recorded as written in
// source; it is NEVER resolved — DNS at scan or generate time would break
// the determinism contract.
func hostFromURLLiteral(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	host := u.Hostname()
	if host == "" {
		return "", false
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return net.JoinHostPort(host, port), true
}

// setToSorted converts a capture set to the sorted, deduped slice the
// inventory stores (determinism contract). Returns nil for an empty set so
// the omitempty JSON fields stay absent.
func setToSorted(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
