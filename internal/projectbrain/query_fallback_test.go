package projectbrain

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestQueryFallbackCases(t *testing.T) {
	data, err := os.ReadFile("query_fallback_cases.json")
	if err != nil {
		t.Fatalf("read cases: %v", err)
	}
	var cases []struct {
		Input string
		Want  string
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("decode cases: %v", err)
	}
	for i, c := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if got := fallbackQuery(c.Input); got != c.Want {
				t.Fatalf("input=%q got=%q want=%q", c.Input, got, c.Want)
			}
		})
	}
}

func TestQueryFallbackBounds(t *testing.T) {
	if got := fallbackQuery(strings.Repeat("ก", 4096) + " load_record"); got != "" {
		t.Fatalf("long thai query: got %q", got)
	}
	q := fallbackQuery(strings.Repeat("a", 10000) + "_entry")
	if q == "" {
		t.Fatal("empty fallback query")
	}
	if len(q) > 512 {
		t.Fatalf("query too long: %d", len(q))
	}
	if !utf8.ValidString(q) {
		t.Fatal("query not valid UTF-8")
	}
	if len(strings.Fields(q)) > 2 {
		t.Fatalf("too many fields: %d", len(strings.Fields(q)))
	}
	if got := fallbackQuery(""); got != "" {
		t.Fatalf("empty query: got %q", got)
	}
}
