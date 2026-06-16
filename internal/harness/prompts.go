package harness

import (
	"encoding/json"
	"strings"
)

func ComposeSystemPrompt(
	workspace Workspace,
	catalog map[string][]map[string]any,
	skills *SkillRegistry,
	references []ReferencedFile,
	observations []Observation,
) string {
	context := map[string]any{
		"current_project":    workspace.Project,
		"sandbox_path":       workspace.SandboxPath,
		"mode":               workspace.Mode,
		"available_toolsets": catalog,
		"available_skills":   skills.List(),
		"referenced_files":   references,
		"observations":       observations,
	}
	contextJSON, _ := json.MarshalIndent(context, "", "  ")
	parts := []string{
		strings.TrimSpace(ReadPrompt("rules.md")),
		strings.TrimSpace(ReadPrompt("main.md")),
		"<harness_context>\n" + string(contextJSON) + "\n</harness_context>",
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
