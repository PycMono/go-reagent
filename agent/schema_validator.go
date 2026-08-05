package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/PycMono/go-reagent/ai"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

func compileSchemaValidator(definition ai.ToolDefinition) (func(json.RawMessage) error, error) {
	schemaJSON, err := json.Marshal(definition.InputSchema)
	if err != nil {
		return nil, fmt.Errorf("marshal input schema for tool %q: %w", definition.Name, err)
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
	if err != nil {
		return nil, fmt.Errorf("decode input schema for tool %q: %w", definition.Name, err)
	}
	location := "urn:go-reagent:tool:" + definition.Name
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(location, document); err != nil {
		return nil, fmt.Errorf("register input schema for tool %q: %w", definition.Name, err)
	}
	compiled, err := compiler.Compile(location)
	if err != nil {
		return nil, fmt.Errorf("compile input schema for tool %q: %w", definition.Name, err)
	}

	return func(arguments json.RawMessage) error {
		decoder := json.NewDecoder(bytes.NewReader(arguments))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return fmt.Errorf("invalid arguments for tool %q: %w", definition.Name, err)
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			if err != nil {
				return fmt.Errorf("invalid trailing arguments for tool %q: %w", definition.Name, err)
			}
			return fmt.Errorf("invalid trailing arguments for tool %q", definition.Name)
		}
		if err := compiled.Validate(value); err != nil {
			return fmt.Errorf("arguments do not match schema for tool %q: %w", definition.Name, err)
		}
		return nil
	}, nil
}
