package providers

import (
	"encoding/json"
	"fmt"

	"github.com/PycMono/go-reagent/pi/ai"
)

type normalizedToolDefinition struct {
	name        string
	description string
	inputSchema map[string]any
}

func normalizeToolDefinitions(definitions []ai.ToolDefinition) ([]normalizedToolDefinition, error) {
	result := make([]normalizedToolDefinition, 0, len(definitions))
	for _, definition := range definitions {
		object, err := schemaObject(definition.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("tool %q input schema: %w", definition.Name, err)
		}
		if schemaType, exists := object["type"]; exists && schemaType != "object" {
			return nil, fmt.Errorf("tool %q input schema type must be object", definition.Name)
		}
		result = append(result, normalizedToolDefinition{
			name: definition.Name, description: definition.Description, inputSchema: object,
		})
	}
	return result, nil
}

func schemaObject(value any) (map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	if object, ok := value.(map[string]any); ok {
		normalized, err := normalizeSchemaNumbers(object)
		if err != nil {
			return nil, err
		}
		return normalized.(map[string]any), nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		return nil, err
	}
	if object == nil {
		return nil, fmt.Errorf("JSON schema must be an object")
	}
	return object, nil
}

func normalizeSchemaNumbers(value any) (any, error) {
	switch typed := value.(type) {
	case json.Number:
		if integer, err := typed.Int64(); err == nil {
			return integer, nil
		}
		number, err := typed.Float64()
		if err != nil {
			return nil, fmt.Errorf("invalid JSON schema number %q: %w", typed, err)
		}
		return number, nil
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			normalized, err := normalizeSchemaNumbers(child)
			if err != nil {
				return nil, err
			}
			result[key] = normalized
		}
		return result, nil
	case []any:
		result := make([]any, len(typed))
		for index, child := range typed {
			normalized, err := normalizeSchemaNumbers(child)
			if err != nil {
				return nil, err
			}
			result[index] = normalized
		}
		return result, nil
	default:
		return value, nil
	}
}

func stringValues(value any) ([]string, error) {
	switch values := value.(type) {
	case nil:
		return nil, nil
	case []string:
		return values, nil
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("must contain only strings")
			}
			result = append(result, text)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("must be an array of strings")
	}
}
