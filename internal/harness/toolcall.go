package harness

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const openToolCall = "<harness_tool_call>"
const closeToolCall = "</harness_tool_call>"

var toolNameRE = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)

func ParseToolCalls(message string) ([]HarnessCall, error) {
	lines := strings.Split(message, "\n")
	var calls []HarnessCall
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != openToolCall {
			continue
		}
		start := i
		i++
		var body []string
		for i < len(lines) && strings.TrimSpace(lines[i]) != closeToolCall {
			body = append(body, lines[i])
			i++
		}
		if i >= len(lines) {
			return nil, fmt.Errorf("missing closing %s for tool call at line %d", closeToolCall, start+1)
		}
		var payload struct {
			Tool string         `json:"tool"`
			Args map[string]any `json:"args"`
		}
		rawBody := strings.TrimSpace(strings.Join(body, "\n"))
		if err := json.Unmarshal([]byte(rawBody), &payload); err != nil {
			return nil, fmt.Errorf("tool call JSON is invalid: %w", err)
		}
		if !toolNameRE.MatchString(payload.Tool) {
			return nil, fmt.Errorf("tool must be in form toolset.tool: %q", payload.Tool)
		}
		if payload.Args == nil {
			return nil, fmt.Errorf("tool args must be a JSON object")
		}
		calls = append(calls, HarnessCall{
			Index: len(calls),
			Tool:  payload.Tool,
			Args:  payload.Args,
			Raw:   strings.Join(lines[start:i+1], "\n"),
		})
	}
	return calls, nil
}
