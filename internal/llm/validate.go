package llm

import (
	"fmt"
	"regexp"
)

var keyPatterns = map[string]*regexp.Regexp{
	"anthropic": regexp.MustCompile(`^sk-ant-[A-Za-z0-9_-]{20,}$`),
}

// ValidateKey checks that key matches the expected format for provider.
// Returns nil for unknown providers — validation is deferred to the provider at call time.
func ValidateKey(provider, key string) error {
	if key == "" {
		return fmt.Errorf("API key must not be empty")
	}
	pattern, ok := keyPatterns[provider]
	if !ok {
		return nil
	}
	if !pattern.MatchString(key) {
		return fmt.Errorf("invalid API key format for %s (expected format: sk-ant-...)", provider)
	}
	return nil
}
