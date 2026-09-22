package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

func decodeSingleProviderJSON(reader io.Reader, provider string, target any) error {
	limited := &io.LimitedReader{R: reader, N: maxProviderResponseBytes + 1}
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s response: %w", provider, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("decode %s response: multiple JSON values", provider)
		}
		return fmt.Errorf("decode %s response: %w", provider, err)
	}
	if limited.N <= 0 {
		return fmt.Errorf("%s response exceeds %d bytes", provider, maxProviderResponseBytes)
	}
	return nil
}

func validateCompletionToolCalls(completion Completion) string {
	for index, call := range completion.ToolCalls {
		if strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Name) == "" {
			return fmt.Sprintf("tool call %d is missing its id or name", index)
		}
		decoder := json.NewDecoder(strings.NewReader(call.Arguments))
		var arguments map[string]any
		if err := decoder.Decode(&arguments); err != nil || arguments == nil {
			return fmt.Sprintf("tool call %d has incomplete arguments", index)
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			return fmt.Sprintf("tool call %d arguments are not exactly one JSON object", index)
		}
	}
	return ""
}
