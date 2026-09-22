package agentplatform

import (
	"bytes"
	"embed"
	"fmt"
	"sync"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed schemas/*.json
var schemaFiles embed.FS

var schemaCache sync.Map

type denySchemaNetwork struct{}

func (denySchemaNetwork) Load(target string) (any, error) {
	return nil, fmt.Errorf("external schema loading is disabled: %s", target)
}

func validateSchema(name string, raw []byte) error {
	compiled, err := compiledSchema(name)
	if err != nil {
		return err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	return compiled.Validate(instance)
}

func compiledSchema(name string) (*jsonschema.Schema, error) {
	value, ok := schemaCache.Load(name)
	if !ok {
		document, err := schemaFiles.ReadFile("schemas/" + name + ".json")
		if err != nil {
			return nil, err
		}
		parsed, err := jsonschema.UnmarshalJSON(bytes.NewReader(document))
		if err != nil {
			return nil, err
		}
		compiler := jsonschema.NewCompiler()
		compiler.DefaultDraft(jsonschema.Draft2020)
		compiler.UseLoader(denySchemaNetwork{})
		location := "urn:hermetrix:agent-platform:" + name
		if err = compiler.AddResource(location, parsed); err != nil {
			return nil, err
		}
		compiled, err := compiler.Compile(location)
		if err != nil {
			return nil, err
		}
		value, _ = schemaCache.LoadOrStore(name, compiled)
	}
	return value.(*jsonschema.Schema), nil
}
