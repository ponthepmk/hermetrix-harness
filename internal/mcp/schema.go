package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

type denyExternalSchemaLoader struct{}

func (denyExternalSchemaLoader) Load(target string) (any, error) {
	return nil, fmt.Errorf("external JSON Schema reference is disabled: %s", target)
}

func compileJSONSchema(raw json.RawMessage) (*jsonschema.Schema, error) {
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.UseLoader(denyExternalSchemaLoader{})
	const location = "urn:hermetrix:mcp-schema"
	if err := compiler.AddResource(location, document); err != nil {
		return nil, err
	}
	return compiler.Compile(location)
}

func decodeSchemaInstance(raw json.RawMessage) (any, error) {
	return jsonschema.UnmarshalJSON(bytes.NewReader(raw))
}

func validateStructuredOutput(schema *jsonschema.Schema, raw json.RawMessage) error {
	result, err := decodeSchemaInstance(raw)
	if err != nil {
		return fmt.Errorf("decode MCP tool result: %w", err)
	}
	object, ok := result.(map[string]any)
	if !ok {
		return fmt.Errorf("MCP tool result must be an object when outputSchema is declared")
	}
	structured, exists := object["structuredContent"]
	if !exists {
		return fmt.Errorf("MCP tool omitted structuredContent required by outputSchema")
	}
	if err := schema.Validate(structured); err != nil {
		return fmt.Errorf("MCP structuredContent violates outputSchema: %w", err)
	}
	return nil
}
