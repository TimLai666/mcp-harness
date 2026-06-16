package harness

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
)

func ValidateExternalMCPArgs(inputSchema any, args map[string]any) error {
	if inputSchema == nil {
		return nil
	}
	schema, err := normalizeSchema(inputSchema)
	if err != nil {
		return fmt.Errorf("invalid inputSchema: %w", err)
	}
	if len(schema) == 0 {
		return nil
	}
	return validateJSONValue("$", args, schema)
}

func normalizeSchema(value any) (map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if string(data) == "null" {
		return nil, nil
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, err
	}
	return schema, nil
}

func validateJSONValue(path string, value any, schema map[string]any) error {
	if enumValues, ok := schema["enum"].([]any); ok && !matchesEnum(value, enumValues) {
		return fmt.Errorf("%s must be one of %s", path, enumText(enumValues))
	}
	if typ, ok := schemaTypes(schema["type"]); ok && !matchesJSONType(value, typ) {
		return fmt.Errorf("%s must be %s", path, strings.Join(typ, " or "))
	}
	if props, ok := schemaMap(schema["properties"]); ok {
		obj, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be object", path)
		}
		for _, key := range requiredKeys(schema["required"]) {
			if _, exists := obj[key]; !exists {
				return fmt.Errorf("%s.%s is required", path, key)
			}
		}
		for key, propSchema := range props {
			if child, exists := obj[key]; exists {
				if err := validateJSONValue(path+"."+key, child, propSchema); err != nil {
					return err
				}
			}
		}
		if additional, exists := schema["additionalProperties"]; exists {
			if allow, ok := additional.(bool); ok && !allow {
				var unknown []string
				for key := range obj {
					if _, known := props[key]; !known {
						unknown = append(unknown, key)
					}
				}
				if len(unknown) > 0 {
					sort.Strings(unknown)
					return fmt.Errorf("%s unknown property/properties: %s", path, strings.Join(unknown, ", "))
				}
			} else if additionalSchema, ok := additional.(map[string]any); ok {
				for key, child := range obj {
					if _, known := props[key]; known {
						continue
					}
					if err := validateJSONValue(path+"."+key, child, additionalSchema); err != nil {
						return err
					}
				}
			}
		}
	}
	if itemSchema, ok := schema["items"].(map[string]any); ok {
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s must be array", path)
		}
		for i, item := range items {
			if err := validateJSONValue(fmt.Sprintf("%s[%d]", path, i), item, itemSchema); err != nil {
				return err
			}
		}
	}
	return nil
}

func schemaTypes(value any) ([]string, bool) {
	switch v := value.(type) {
	case string:
		if v == "" {
			return nil, false
		}
		return []string{v}, true
	case []any:
		var out []string
		for _, item := range v {
			if text, ok := item.(string); ok && text != "" {
				out = append(out, text)
			}
		}
		return out, len(out) > 0
	default:
		return nil, false
	}
}

func schemaMap(value any) (map[string]map[string]any, bool) {
	raw, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	out := map[string]map[string]any{}
	for key, item := range raw {
		if schema, ok := item.(map[string]any); ok {
			out[key] = schema
		}
	}
	return out, true
}

func requiredKeys(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, item := range raw {
		if text, ok := item.(string); ok && text != "" {
			out = append(out, text)
		}
	}
	return out
}

func matchesJSONType(value any, types []string) bool {
	for _, typ := range types {
		switch typ {
		case "null":
			if value == nil {
				return true
			}
		case "object":
			if _, ok := value.(map[string]any); ok {
				return true
			}
		case "array":
			if _, ok := value.([]any); ok {
				return true
			}
		case "string":
			if _, ok := value.(string); ok {
				return true
			}
		case "boolean":
			if _, ok := value.(bool); ok {
				return true
			}
		case "integer":
			if isInteger(value) {
				return true
			}
		case "number":
			if isNumber(value) {
				return true
			}
		}
	}
	return false
}

func isInteger(value any) bool {
	switch v := value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	case float64:
		return math.Trunc(v) == v
	default:
		return false
	}
}

func isNumber(value any) bool {
	switch value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return true
	default:
		return false
	}
}

func matchesEnum(value any, enumValues []any) bool {
	for _, candidate := range enumValues {
		if reflect.DeepEqual(value, candidate) {
			return true
		}
	}
	return false
}

func enumText(values []any) string {
	data, err := json.Marshal(values)
	if err != nil {
		return fmt.Sprint(values)
	}
	return string(data)
}
