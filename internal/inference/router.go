// Package inference is the BYOK proxy + cache from architecture §2.
//
// NON-FUNCTIONAL PLACEHOLDER — do not mistake this for working infrastructure.
// As of today this package is a scaffold: the types and method shapes exist, but
// Call() makes no LLM call (it returns ErrLLMDisabled with no key and a
// "not implemented" error with one), and Router is never instantiated by the
// scan pipeline — nothing outside this package's own tests calls New(). The
// consequence to know: cache.put is unreachable in a real run, so the cache-hit
// branch in Call() can never be taken in production. The "no network call
// without a key" guarantee therefore holds only vacuously (it makes no call at
// all). Treat the cache as inert until the LLM path lands.
//
// To make it real: wire `anthropic-sdk-go` into Call() (CSDK-005 "raw
// exceptions" is the most useful first target — its rule is pattern-easy but the
// *fix* prescription benefits from LLM context), call cache.put on success, and
// instantiate the Router from the scanner. Discipline: every Call MUST honor the
// cache, and the cache key MUST include the model name once we route across
// models (see the TODO in Call).
package inference

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
)

// Request is the shape of one inference call.
type Request struct {
	// ToolASTHash is the deterministic hash of the tool's source. Architecture
	// §3 calls this out as the cache key for keeping repeat scans cheap.
	ToolASTHash string
	System      string
	User        string
}

// Response is what the router returns. Currently a single text payload.
type Response struct {
	Text      string
	FromCache bool
}

// Router is the single point of inference. It owns the cache and the API key
// boundary (BYOK).
type Router struct {
	apiKey string
	cache  *cache
}

// New returns a Router. apiKey is the user's Anthropic/OpenAI key per BYOK;
// pass empty string to disable LLM calls (cache-only operation).
func New(apiKey string) *Router {
	return &Router{apiKey: apiKey, cache: newCache()}
}

// Call runs an inference request. With no API key it returns ErrLLMDisabled.
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
var ErrLLMDisabled = errors.New("inference router: no API key provided (LLM enrichment disabled)")

// ASTHash returns the canonical tool-AST hash for cache lookup.
func ASTHash(canonicalSource string) string {
	sum := sha256.Sum256([]byte(canonicalSource))
	return hex.EncodeToString(sum[:])
}

// ────────────────────────────────────────────────────────────────────────────
// cache: in-memory, process-local.
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
