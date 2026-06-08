package enrichment

import "strings"

// extractScope returns the enclosing function or class block around line (1-based).
// Falls back to line ± contextLines if no enclosing block is found.
func extractScope(content string, line, contextLines int) string {
	lines := strings.Split(content, "\n")
	if line < 1 || line > len(lines) {
		return scopeFallback(lines, line, contextLines)
	}

	idx := line - 1

	blockStart := -1
	for i := idx; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if isBlockHeader(trimmed) {
			blockStart = i
			// Keep searching if this is a method (def) to find an enclosing class
			if strings.HasPrefix(trimmed, "class ") {
				break
			}
			// For methods, keep searching for a potential class
			if strings.HasPrefix(trimmed, "def ") || strings.HasPrefix(trimmed, "async def ") {
				// Continue searching to see if there's a class
				foundClass := false
				methodIndent := indentLevel(lines[i])
				for j := i - 1; j >= 0; j-- {
					jTrimmed := strings.TrimSpace(lines[j])
					if strings.HasPrefix(jTrimmed, "class ") && indentLevel(lines[j]) < methodIndent {
						blockStart = j
						foundClass = true
						break
					}
				}
				if foundClass {
					break
				}
				break
			}
			break
		}
	}

	if blockStart == -1 {
		return scopeFallback(lines, line, contextLines)
	}

	headerIndent := indentLevel(lines[blockStart])
	blockEnd := len(lines) - 1
	for i := blockStart + 1; i < len(lines); i++ {
		l := lines[i]
		if strings.TrimSpace(l) == "" {
			continue
		}
		if indentLevel(l) <= headerIndent && i > blockStart+1 {
			blockEnd = i - 1
			break
		}
	}

	var sb strings.Builder
	for i := blockStart; i <= blockEnd; i++ {
		marker := "  "
		if i+1 == line {
			marker = "→ "
		}
		sb.WriteString(marker)
		sb.WriteString(lines[i])
		sb.WriteString("\n")
	}
	return sb.String()
}

func isBlockHeader(trimmed string) bool {
	return strings.HasPrefix(trimmed, "def ") ||
		strings.HasPrefix(trimmed, "async def ") ||
		strings.HasPrefix(trimmed, "class ") ||
		strings.HasPrefix(trimmed, "function ") ||
		strings.HasPrefix(trimmed, "async function ") ||
		(strings.HasPrefix(trimmed, "const ") && strings.Contains(trimmed, "=>")) ||
		strings.HasPrefix(trimmed, "export function ") ||
		strings.HasPrefix(trimmed, "export async function ")
}

func indentLevel(line string) int {
	count := 0
	for _, ch := range line {
		if ch == ' ' {
			count++
		} else if ch == '\t' {
			count += 4
		} else {
			break
		}
	}
	return count
}

func scopeFallback(lines []string, line, contextLines int) string {
	start := line - 1 - contextLines
	if start < 0 {
		start = 0
	}
	end := line - 1 + contextLines
	if end >= len(lines) {
		end = len(lines) - 1
	}
	var sb strings.Builder
	for i := start; i <= end; i++ {
		marker := "  "
		if i+1 == line {
			marker = "→ "
		}
		sb.WriteString(marker)
		sb.WriteString(lines[i])
		sb.WriteString("\n")
	}
	return sb.String()
}
