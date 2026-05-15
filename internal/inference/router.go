// Package inference is the BYOK proxy + cache from architecture §2.
//
// STATUS: stub. The interface and cache shape are real; no LLM call is made.
// Wire `anthropic-sdk-go` into Call() when the LLM enrichment pass is ready
// (CSDK-005 "raw exceptions" is the most useful first target — its rule is
// pattern-easy but the *fix* prescription benefits from LLM context).
package inference

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
)

// Request is the shape of one inference call. Kept minimal until we have a
// real call to model.
type Request struct {
	// ToolASTHash is the deterministic hash of the tool's source. Architecture
	// §3 calls this out as the cache key for keeping repeat scans cheap.
	ToolASTHash string
	System      string
	User        string
}

// Response is what the router returns. Currently a single text payload.
type Response struct {
	Text       string
	FromCache  bool
}

// Router is the single point of inference. It owns the cache and the API key
// boundary (BYOK).
type Router struct {
	apiKey string
	cache  *cache
}

// New returns a Router. apiKey is the user's Anthropic/OpenAI key per BYOK;
// pass empty string to operate in stub mode (no calls will be made).
func New(apiKey string) *Router {
	return &Router{apiKey: apiKey, cache: newCache()}
}

// Call runs an inference request. In stub mode it returns ErrLLMDisabled.
func (r *Router) Call(_ context.Context, req Request) (*Response, error) {
	if v, ok := r.cache.get(req.ToolASTHash); ok {
		return &Response{Text: v, FromCache: true}, nil
	}
	if r.apiKey == "" {
		return nil, ErrLLMDisabled
	}
	// TODO: wire anthropic-sdk-go here. Discipline: every Call MUST honor the
	// cache, and the cache key MUST include the model name once we route
	// across models. Skipping the model in the key is the most common cache
	// poisoning bug for proxies like this.
	return nil, errors.New("inference router: LLM call not implemented")
}

// ErrLLMDisabled is returned when no API key was provided. Callers should
// treat this as a soft skip, not a scan failure.
var ErrLLMDisabled = errors.New("inference router: no API key provided (running in stub mode)")

// ASTHash returns the canonical tool-AST hash for cache lookup.
func ASTHash(canonicalSource string) string {
	sum := sha256.Sum256([]byte(canonicalSource))
	return hex.EncodeToString(sum[:])
}

// ────────────────────────────────────────────────────────────────────────────
// cache: in-memory, process-local. Sufficient for v0.1.
// Swap for a disk-backed cache once scans cross process boundaries (CI runs).
// ────────────────────────────────────────────────────────────────────────────

type cache struct {
	mu sync.RWMutex
	m  map[string]string
}

func newCache() *cache { return &cache{m: map[string]string{}} }

func (c *cache) get(k string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.m[k]
	return v, ok
}

func (c *cache) put(k, v string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[k] = v
}
