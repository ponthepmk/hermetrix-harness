package agentplatform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// CanonicalizeJSON implements the RFC 8785 rules needed by the locked H1
// schemas. H1 accepts only integer JSON numbers, avoiding lossy binary-float
// conversion while still covering every numeric field in its contract subset.
func CanonicalizeJSON(raw []byte) ([]byte, error) {
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("JSON is not valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := decodeUnique(decoder)
	if err != nil {
		return nil, err
	}
	if token, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("unexpected trailing JSON token %v", token)
		}
		return nil, fmt.Errorf("read trailing JSON: %w", err)
	}
	var out bytes.Buffer
	if err := writeCanonical(&out, value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func decodeUnique(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}
	switch delim {
	case '{':
		object := map[string]any{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("object key is not a string")
			}
			if _, exists := object[key]; exists {
				return nil, fmt.Errorf("duplicate JSON object key %q", key)
			}
			value, err := decodeUnique(decoder)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
			return nil, fmt.Errorf("unterminated JSON object")
		}
		return object, nil
	case '[':
		var values []any
		for decoder.More() {
			value, err := decodeUnique(decoder)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		if end, err := decoder.Token(); err != nil || end != json.Delim(']') {
			return nil, fmt.Errorf("unterminated JSON array")
		}
		return values, nil
	default:
		return nil, fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
}

func writeCanonical(out *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		out.WriteString(strconv.FormatBool(typed))
	case string:
		writeJSONString(out, typed)
	case json.Number:
		number := typed.String()
		if number == "-0" {
			number = "0"
		}
		if !validInteger(number) {
			return fmt.Errorf("H1 canonical JSON accepts integer contract numbers only: %q", number)
		}
		out.WriteString(number)
	case []any:
		out.WriteByte('[')
		for i, item := range typed {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := writeCanonical(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool { return utf16Less(keys[i], keys[j]) })
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			writeJSONString(out, key)
			out.WriteByte(':')
			if err := writeCanonical(out, typed[key]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		return fmt.Errorf("unsupported canonical JSON value %T", value)
	}
	return nil
}

func validInteger(value string) bool {
	if value == "0" {
		return true
	}
	original := value
	if strings.HasPrefix(value, "-") {
		value = value[1:]
	}
	if value == "" || value[0] == '0' {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	parsed, ok := new(big.Int).SetString(original, 10)
	if !ok {
		return false
	}
	limit := big.NewInt(9007199254740991)
	return new(big.Int).Abs(parsed).Cmp(limit) <= 0
}

func utf16Less(left, right string) bool {
	a, b := utf16.Encode([]rune(left)), utf16.Encode([]rune(right))
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

func writeJSONString(out *bytes.Buffer, value string) {
	out.WriteByte('"')
	for len(value) > 0 {
		r, size := utf8.DecodeRuneInString(value)
		value = value[size:]
		switch r {
		case '"', '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		case '\b':
			out.WriteString(`\b`)
		case '\t':
			out.WriteString(`\t`)
		case '\n':
			out.WriteString(`\n`)
		case '\f':
			out.WriteString(`\f`)
		case '\r':
			out.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(out, `\u%04x`, r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
}

func DigestJSON(raw []byte) (string, []byte, error) {
	canonical, err := CanonicalizeJSON(raw)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), canonical, nil
}
