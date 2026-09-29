package projectbrain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// candidateJCSDigest uses RFC 8785's UTF-16 key order and ECMAScript string
// escaping for the closed KnowledgeCandidate wire shape. Its only number is
// schema_revision (and task_revision in the local identity), both integers in
// the I-JSON exact range. No arbitrary floating-point input is accepted.
func candidateJCSDigest(raw []byte) (string, error) {
	if !utf8.Valid(raw) {
		return "", errors.New("candidate JSON is not UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return "", errors.New("candidate JSON has trailing data")
	}
	var canonical bytes.Buffer
	if err := writeCandidateJCS(&canonical, value); err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical.Bytes())
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func writeCandidateJCS(out *bytes.Buffer, value any) error {
	switch item := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		out.WriteString(strconv.FormatBool(item))
	case string:
		writeCandidateString(out, item)
	case json.Number:
		number := item.String()
		if number == "-0" {
			number = "0"
		}
		parsed, err := strconv.ParseInt(number, 10, 64)
		if err != nil || strconv.FormatInt(parsed, 10) != number || parsed < -9007199254740991 || parsed > 9007199254740991 {
			return fmt.Errorf("candidate JSON number is outside the JCS exact integer subset: %q", number)
		}
		out.WriteString(number)
	case []any:
		out.WriteByte('[')
		for i, child := range item {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := writeCandidateJCS(out, child); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(item))
		for key := range item {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool { return candidateUTF16Less(keys[i], keys[j]) })
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			writeCandidateString(out, key)
			out.WriteByte(':')
			if err := writeCandidateJCS(out, item[key]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		return fmt.Errorf("unsupported candidate JSON value %T", value)
	}
	return nil
}

func candidateUTF16Less(left, right string) bool {
	a, b := utf16.Encode([]rune(left)), utf16.Encode([]rune(right))
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

func writeCandidateString(out *bytes.Buffer, value string) {
	out.WriteByte('"')
	for _, char := range value {
		switch char {
		case '"', '\\':
			out.WriteByte('\\')
			out.WriteRune(char)
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
			if char < 0x20 {
				fmt.Fprintf(out, `\u%04x`, char)
			} else {
				out.WriteRune(char)
			}
		}
	}
	out.WriteByte('"')
}
