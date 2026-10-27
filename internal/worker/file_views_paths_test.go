package worker

import (
	"strings"
	"testing"
)

func TestBoundedFileViewCanonicalPaths(t *testing.T) {
	for _, p := range []string{"internal/sub/file.go", ".gitignore", "ไทย/relative.go"} {
		v, err := NewBoundedFileView(p, "abc", Hash("abc"), 0, 3)
		if err != nil {
			t.Fatalf("%s: NewBoundedFileView: %v", p, err)
		}
		if err := ValidateBoundedFileView(p, "abc", Hash("abc"), v); err != nil {
			t.Fatalf("%s: ValidateBoundedFileView: %v", p, err)
		}
	}
	for _, p := range []string{"", ".", "..", "../x.go", "a/../x.go", "./x.go", "/x.go", "a//x.go", "a\\x.go", "C:/x.go", "x\ny.go", "x\u0085y.go", "x\xffy.go"} {
		if _, err := NewBoundedFileView(p, "abc", Hash("abc"), 0, 3); err == nil {
			t.Fatalf("%q: expected error", p)
		}
	}
}

func TestBoundedFileViewUnicodeAndIdentity(t *testing.T) {
	src := "head\r\nไทยé尾\n"
	v, err := NewBoundedFileView("u.txt", src, Hash(src), 0, len(src))
	if err != nil {
		t.Fatalf("full: %v", err)
	}
	if v.Content != src || v.RegionID == "" {
		t.Fatalf("full content/region mismatch")
	}
	i := strings.Index(src, "ไทย")
	w, err := NewBoundedFileView("u.txt", src, Hash(src), i, i+len("ไทย"))
	if err != nil {
		t.Fatalf("partial: %v", err)
	}
	if w.Content != "ไทย" || w.RegionID == "" {
		t.Fatalf("partial content/region mismatch")
	}
	if err := ValidateBoundedFileView("u.txt", src, Hash(src), v); err != nil {
		t.Fatalf("full ValidateBoundedFileView: %v", err)
	}
	if err := ValidateBoundedFileView("u.txt", src, Hash(src), w); err != nil {
		t.Fatalf("partial ValidateBoundedFileView: %v", err)
	}
	if v.RegionID == w.RegionID {
		t.Fatalf("identical inputs produced same region ID")
	}
	v2, err := NewBoundedFileView("u.txt", src, Hash(src), 0, len(src))
	if err != nil || v2.RegionID != v.RegionID {
		t.Fatalf("identical inputs must produce identical region ID")
	}
}
