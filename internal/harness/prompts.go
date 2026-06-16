package harness

import (
	"encoding/json"
	"strings"
)

func ComposeSystemPrompt(
	workspace Workspace,
	accessMode AccessMode,
	catalog map[string][]map[string]any,
	skills *SkillRegistry,
	references []ReferencedFile,
	instructions []ProjectInstruction,
	observations []Observation,
	activeSkillNames []string,
) string {
	activeSkills := []map[string]any{}
	for _, name := range activeSkillNames {
		skill, err := skills.Get(name)
		if err != nil {
			continue
		}
		activeSkills = append(activeSkills, map[string]any{
			"name":    skill.Name,
			"content": skill.content,
		})
	}
	context := map[string]any{
		"current_project":      workspace.Project,
		"sandbox_path":         workspace.SandboxPath,
		"mode":                 workspace.Mode,
		"access_mode":          accessMode,
		"available_toolsets":   catalog,
		"available_skills":     skills.List(),
		"active_skills":        activeSkills,
		"referenced_files":     references,
		"project_instructions": instructions,
		"observations":         observations,
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
