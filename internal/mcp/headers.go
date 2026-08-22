package mcp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

var headerToken = regexp.MustCompile(`^[!#$%&'*+\-.^_` + "`" + `|~0-9A-Za-z]+$`)

type headerBinding struct {
	Name string
	Path []string
	Type string
}

func validateAndCollectHeaderBindings(schema json.RawMessage) ([]headerBinding, error) {
	var root map[string]any
	if err := json.Unmarshal(schema, &root); err != nil {
		return nil, fmt.Errorf("invalid input schema: %w", err)
	}
	bindings := []headerBinding{}
	seen := map[string]bool{}
	var walk func(map[string]any, []string, bool) error
	walk = func(node map[string]any, path []string, reachable bool) error {
		if rawName, exists := node["x-mcp-header"]; exists {
			name, ok := rawName.(string)
			kind, typeOK := node["type"].(string)
			lower := strings.ToLower(name)
			if !reachable || len(path) == 0 || !ok || !typeOK || !headerToken.MatchString(name) ||
				(kind != "string" && kind != "integer" && kind != "boolean") || seen[lower] {
				return fmt.Errorf("invalid x-mcp-header annotation at %s", strings.Join(path, "."))
			}
			seen[lower] = true
			bindings = append(bindings, headerBinding{Name: name, Path: append([]string(nil), path...), Type: kind})
		}
		for key, value := range node {
			switch key {
			case "properties":
				properties, ok := value.(map[string]any)
				if !ok {
					continue
				}
				for propertyName, rawProperty := range properties {
					property, ok := rawProperty.(map[string]any)
					if !ok {
						continue
					}
					if err := walk(property, append(path, propertyName), reachable); err != nil {
						return err
					}
				}
			case "x-mcp-header":
				continue
			default:
				if child, ok := value.(map[string]any); ok {
					if err := walk(child, path, false); err != nil {
						return err
					}
				}
				if children, ok := value.([]any); ok {
					for _, rawChild := range children {
						if child, ok := rawChild.(map[string]any); ok {
							if err := walk(child, path, false); err != nil {
								return err
							}
						}
					}
				}
			}
		}
		return nil
	}
	if err := walk(root, nil, true); err != nil {
		return nil, err
	}
	return bindings, nil
}

func customToolHeaders(toolName string, arguments, schema json.RawMessage) (http.Header, error) {
	bindings, err := validateAndCollectHeaderBindings(schema)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	if err := json.Unmarshal(arguments, &object); err != nil {
		return nil, fmt.Errorf("tool arguments must be an object: %w", err)
	}
	headers := http.Header{}
	headers.Set("Mcp-Name", encodeHeaderValue(toolName))
	for _, binding := range bindings {
		value, exists := nestedValue(object, binding.Path)
		if !exists || value == nil {
			continue
		}
		encoded, err := primitiveHeaderValue(value, binding.Type)
		if err != nil {
			return nil, fmt.Errorf("Mcp-Param-%s: %w", binding.Name, err)
		}
		headers.Set("Mcp-Param-"+binding.Name, encodeHeaderValue(encoded))
	}
	return headers, nil
}

func nestedValue(root map[string]any, path []string) (any, bool) {
	var current any = root
	for _, segment := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func primitiveHeaderValue(value any, kind string) (string, error) {
	switch kind {
	case "string":
		text, ok := value.(string)
		if !ok {
			return "", fmt.Errorf("value is not a string")
		}
		return text, nil
	case "boolean":
		flag, ok := value.(bool)
		if !ok {
			return "", fmt.Errorf("value is not a boolean")
		}
		return strconv.FormatBool(flag), nil
	case "integer":
		number, ok := value.(float64)
		if !ok || number != float64(int64(number)) || number < -9007199254740991 || number > 9007199254740991 {
			return "", fmt.Errorf("value is not a safe integer")
		}
		return strconv.FormatInt(int64(number), 10), nil
	default:
		return "", fmt.Errorf("unsupported primitive type")
	}
}

func encodeHeaderValue(value string) string {
	if safePlainHeaderValue(value) && !(strings.HasPrefix(value, "=?base64?") && strings.HasSuffix(value, "?=")) {
		return value
	}
	return "=?base64?" + base64.StdEncoding.EncodeToString([]byte(value)) + "?="
}

func safePlainHeaderValue(value string) bool {
	if strings.TrimSpace(value) != value {
		return false
	}
	for _, char := range []byte(value) {
		if char < 0x20 || char > 0x7e {
			return false
		}
	}
	return true
}
