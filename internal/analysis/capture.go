package analysis

import (
	"net"
	"net/url"
	"sort"

	"github.com/trustabl/trustabl/internal/models"
)

// httpCallKey is the dedup key for an HTTP call record.
func httpCallKey(c models.HTTPCall) string {
	return c.HostPort + "\x00" + c.Method + "\x00" + c.Path
}

// sortedHTTPCalls converts an HTTP-call set to a deterministic slice, sorted by
// (host:port, method, path). Returns nil for an empty set (omitempty stays
// absent).
func sortedHTTPCalls(set map[string]models.HTTPCall) []models.HTTPCall {
	if len(set) == 0 {
		return nil
	}
	out := make([]models.HTTPCall, 0, len(set))
	for _, c := range set {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].HostPort != out[j].HostPort {
			return out[i].HostPort < out[j].HostPort
		}
		if out[i].Method != out[j].Method {
			return out[i].Method < out[j].Method
		}
		return out[i].Path < out[j].Path
	})
	return out
}

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

// hostPathFromURLLiteral parses a static URL literal and returns its canonical
// host:port AND its URL path. Same qualification and determinism rules as
// hostFromURLLiteral (absolute http/https, never DNS-resolved); the path is the
// URL's path component with any query/fragment dropped. A root or absent path
// returns "" for the path (the OpenShell exporter treats that as "cannot scope
// by path" and falls back to the coarse access preset).
func hostPathFromURLLiteral(raw string) (hostPort, path string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", "", false
	}
	host := u.Hostname()
	if host == "" {
		return "", "", false
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	p := u.Path
	if p == "/" {
		p = ""
	}
	return net.JoinHostPort(host, port), p, true
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
