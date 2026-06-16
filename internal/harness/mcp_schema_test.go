package harness

import (
	"strings"
	"testing"
)

func TestValidateExternalMCPArgs(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"days": map[string]any{"type": "integer"},
			"tags": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"mode": map[string]any{"enum": []any{"fast", "safe"}},
		},
		"required":             []any{"name"},
		"additionalProperties": false,
	}
	if err := ValidateExternalMCPArgs(schema, map[string]any{
		"name": "Ada",
		"days": float64(2),
		"tags": []any{"go"},
		"mode": "safe",
	}); err != nil {
		t.Fatalf("expected schema validation to pass: %v", err)
	}
	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "missing required",
			args: map[string]any{},
			want: "$.name is required",
		},
		{
			name: "wrong type",
			args: map[string]any{"name": 42},
			want: "$.name must be string",
		},
		{
			name: "unknown property",
			args: map[string]any{"name": "Ada", "extra": true},
			want: "unknown property/properties: extra",
		},
		{
			name: "enum",
			args: map[string]any{"name": "Ada", "mode": "risky"},
			want: "$.mode must be one of",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateExternalMCPArgs(schema, tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}
