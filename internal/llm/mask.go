package llm

// MaskKey returns a masked representation of key for display.
// Keys longer than 4 chars show "****...{last4}"; shorter or equal show "****"; empty shows "(not set)".
func MaskKey(key string) string {
	if key == "" {
		return "(not set)"
	}
	if len(key) <= 4 {
		return "****"
	}
	return "****..." + key[len(key)-4:]
}
