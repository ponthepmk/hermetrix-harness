package worker

import (
	"testing"
)

func TestBoundedFileViewExhaustiveMatrix(t *testing.T) {
	source := []byte{97, 13, 10, 195, 169, 122}
	fullSource := string(source)
	fullSHA := Hash(fullSource)
	allowed := []int{0, 1, 3, 5, 6}
	valid := 0
	invalid := 0
	for _, s := range allowed {
		for _, e := range allowed {
			if s > e {
				continue
			}
			v, err := NewBoundedFileView("p", fullSource, fullSHA, s, e)
			if err != nil {
				t.Fatalf("valid %d..%d: %v", s, e, err)
			}
			if err := ValidateBoundedFileView("p", fullSource, fullSHA, v); err != nil {
				t.Fatalf("valid %d..%d validate: %v", s, e, err)
			}
			if v.Content != fullSource[s:e] {
				t.Fatalf("valid %d..%d content mismatch", s, e)
			}
			valid++
		}
	}
	for s := -1; s <= 7; s++ {
		for e := -1; e <= 7; e++ {
			if s <= e && contains(allowed, s) && contains(allowed, e) {
				continue
			}
			_, err := NewBoundedFileView("p", fullSource, fullSHA, s, e)
			if err == nil {
				t.Fatalf("invalid %d..%d accepted", s, e)
			}
			invalid++
		}
	}
	if valid != 15 || invalid != 66 {
		t.Fatalf("counts %d/%d", valid, invalid)
	}
}

func contains(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
