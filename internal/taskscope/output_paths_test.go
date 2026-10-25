package taskscope_test

import (
	"fmt"
	"testing"

	"hermetrix-harness/internal/taskscope"
)

func pad(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}

func shortPaths(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("p%03d", i)
	}
	return out
}

func bigPaths(n, each int) []string {
	out := make([]string, n)
	for i := range out {
		prefix := fmt.Sprintf("b%02d_", i)
		out[i] = prefix + pad(each-len(prefix))
	}
	return out
}

func utf8Path(n int) string {
	b := make([]byte, n)
	for i := 0; i+1 < n; i += 2 {
		b[i] = 0xc3
		b[i+1] = 0xa9
	}
	if n%2 == 1 {
		b[n-1] = 'a'
	}
	return string(b)
}

func TestValidateWritablePaths(t *testing.T) {
	tests := []struct {
		name    string
		paths   []string
		wantErr bool
	}{
		{"nil", nil, false},
		{"empty", []string{}, false},
		{"single", []string{"a"}, false},
		{"512_distinct", shortPaths(512), false},
		{"513_distinct", shortPaths(513), true},
		{"total_32768_ok", bigPaths(64, 511), false},
		{"total_32769_fail", func() []string { p := bigPaths(64, 511); p[0] += "x"; return p }(), true},
		{"single_512_ok", []string{pad(512)}, false},
		{"single_513_fail", []string{pad(513)}, true},
		{"utf8_512_ok", []string{utf8Path(512)}, false},
		{"utf8_513_fail", []string{utf8Path(513)}, true},
		{"empty_str", []string{""}, true},
		{"outer_space_l", []string{" a"}, true},
		{"outer_space_r", []string{"a "}, true},
		{"leading_slash", []string{"/a"}, true},
		{"dotdot_seg", []string{"a/../b"}, true},
		{"backslash", []string{"a\\b"}, true},
		{"colon", []string{"a:b"}, true},
		{"star", []string{"a*b"}, true},
		{"question", []string{"a?b"}, true},
		{"cr", []string{"a\rb"}, true},
		{"lf", []string{"a\nb"}, true},
		{"nul", []string{"a\x00b"}, true},
		{"dup_global", []string{"a", "a"}, true},
		{"empty_seg", []string{"a//b"}, true},
		{"dot_seg", []string{"a/./b"}, true},
		{"dot_only", []string{"."}, true},
		{"dotdot_only", []string{".."}, true},
		{"trailing_slash", []string{"a/"}, true},
		{"bracket_ok", []string{"a[1]"}, false},
		{"inner_space_ok", []string{"a b/c"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := taskscope.ValidateWritablePaths(tt.paths)
			if (err != nil) != tt.wantErr {
				t.Fatalf("%s: wantErr=%v got err=%v", tt.name, tt.wantErr, err)
			}
		})
	}
}

func TestValidateOutputPaths(t *testing.T) {
	tests := []struct {
		name     string
		outputs  []string
		writable []string
		wantErr  bool
	}{
		{"nil_both", nil, nil, false},
		{"empty_both", []string{}, []string{}, false},
		{"blank_legacy_32", nil, shortPaths(32), false},
		{"blank_legacy_33", nil, shortPaths(33), true},
		{"explicit_32_in_512", shortPaths(32), shortPaths(512), false},
		{"explicit_33_reject", shortPaths(33), shortPaths(512), true},
		{"explicit_no_auth_nil", []string{"a"}, nil, true},
		{"explicit_no_auth_empty", []string{"a"}, []string{}, true},
		{"unauthorized", []string{"zzz"}, shortPaths(32), true},
		{"invalid_global_subset", []string{"a"}, []string{"a", "bad\\p"}, true},
		{"dup_output", []string{"a", "a"}, []string{"a"}, true},
		{"malformed_output", []string{"a/../b"}, []string{"a"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := taskscope.ValidateOutputPaths(tt.outputs, tt.writable)
			if (err != nil) != tt.wantErr {
				t.Fatalf("%s: wantErr=%v got err=%v", tt.name, tt.wantErr, err)
			}
		})
	}
}

func TestEffectiveWritablePaths(t *testing.T) {
	// outputs non-nil: returns defensive copy of outputs
	out := []string{"a", "b"}
	wr := []string{"a", "b", "c"}
	got, err := taskscope.EffectiveWritablePaths(out, wr)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %v", got)
	}
	got[0] = "mut"
	if out[0] != "a" {
		t.Fatal("input mutated")
	}

	// outputs nil: returns defensive copy of writable
	wr2 := []string{"x", "y"}
	got2, err := taskscope.EffectiveWritablePaths(nil, wr2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got2) != 2 || got2[0] != "x" {
		t.Fatalf("got %v", got2)
	}
	got2[0] = "mut"
	if wr2[0] != "x" {
		t.Fatal("input mutated")
	}

	// both nil: returns nil
	got3, err := taskscope.EffectiveWritablePaths(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got3 != nil {
		t.Fatalf("expected nil, got %v", got3)
	}

	// failed validation must not mutate inputs
	badOut := []string{"a"}
	badWr := []string{"bad\\p"}
	_, err = taskscope.EffectiveWritablePaths(badOut, badWr)
	if err == nil {
		t.Fatal("expected error")
	}
	if badOut[0] != "a" || badWr[0] != "bad\\p" {
		t.Fatal("inputs mutated on failure")
	}

	// nil outputs + non-nil empty writable: non-nil zero-length copy
	emptyWr := []string{}
	got4, err := taskscope.EffectiveWritablePaths(nil, emptyWr)
	if err != nil {
		t.Fatal(err)
	}
	if got4 == nil || len(got4) != 0 {
		t.Fatalf("expected non-nil empty slice, got %v", got4)
	}

	// invalid duplicates must not mutate inputs
	dupOut := []string{"a", "a"}
	dupWr := []string{"a"}
	_, err = taskscope.EffectiveWritablePaths(dupOut, dupWr)
	if err == nil {
		t.Fatal("expected error")
	}
	if dupOut[0] != "a" || dupOut[1] != "a" || dupWr[0] != "a" {
		t.Fatal("inputs mutated on duplicate failure")
	}
}
