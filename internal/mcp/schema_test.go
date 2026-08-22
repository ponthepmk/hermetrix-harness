package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStructuredOutputValidation(t *testing.T) {
	schema, err := compileJSONSchema(json.RawMessage(`{
    "type":"object",
    "properties":{"value":{"type":"string"}},
    "required":["value"],
    "additionalProperties":false
  }`))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateStructuredOutput(schema, json.RawMessage(`{"structuredContent":{"value":"ok"}}`)); err != nil {
		t.Fatalf("valid structured output failed: %v", err)
	}
	for _, raw := range []string{
		`{"content":[{"type":"text","text":"missing"}]}`,
		`{"structuredContent":{"value":42}}`,
		`{"structuredContent":{"value":"ok","extra":true}}`,
	} {
		if err := validateStructuredOutput(schema, json.RawMessage(raw)); err == nil {
			t.Fatalf("invalid output passed: %s", raw)
		}
	}
}

func TestExternalSchemaReferencesAreDenied(t *testing.T) {
	_, err := compileJSONSchema(json.RawMessage(`{"$ref":"https://example.invalid/remote-schema"}`))
	if err == nil || !strings.Contains(err.Error(), "external JSON Schema reference is disabled") {
		t.Fatalf("external reference error = %v", err)
	}
}
