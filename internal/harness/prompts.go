package harness

import "strings"

// ComposeGuide returns the harness protocol prompt: general working rules
// followed by the harness-specific protocol. It carries no runtime context of
// its own; agents discover projects, skills, and tool schemas through the
// dedicated read-only tools and the MCP tool list.
func ComposeGuide() string {
	parts := []string{
		strings.TrimSpace(ReadPrompt("rules.md")),
		strings.TrimSpace(ReadPrompt("main.md")),
	}
	return strings.Join(nonEmpty(parts), "\n\n")
}

func nonEmpty(parts []string) []string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, part)
		}
	}
	return out
}
