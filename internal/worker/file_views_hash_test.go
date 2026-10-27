package worker

import (
	"testing"
)

func TestBoundedFileViewHashInputs(t *testing.T) {
	fixedPath := "internal/x.go"
	fixedSource := "abc"
	fixedStart := 0
	fixedEnd := 3
	correctHash := Hash(fixedSource)

	view, err := NewBoundedFileView(fixedPath, fixedSource, correctHash, fixedStart, fixedEnd)
	if err != nil {
		t.Fatalf("NewBoundedFileView: %v", err)
	}
	if err := ValidateBoundedFileView(fixedPath, fixedSource, correctHash, view); err != nil {
		t.Fatalf("ValidateBoundedFileView: %v", err)
	}

	invalidExpectedHashes := []string{
		"",
		"absent",
		"0000000000000000000000000000000000000000000000000000000000000000",
		"BA7816BF8F01CFEA414140DE5DAE2223B00361A396177A9CB410FF61F20015AD",
		"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015adx",
	}

	for _, expectedFullSHA := range invalidExpectedHashes {
		_, err := NewBoundedFileView(fixedPath, fixedSource, expectedFullSHA, fixedStart, fixedEnd)
		if err == nil {
			t.Fatalf("NewBoundedFileView with expectedFullSHA %q: expected error, got none", expectedFullSHA)
		}
	}

	badSource := string([]byte{0xff})
	badSourceHash := Hash(badSource)
	_, err = NewBoundedFileView(fixedPath, badSource, badSourceHash, 0, 1)
	if err == nil {
		t.Fatalf("NewBoundedFileView with invalid UTF-8 source: expected error, got none")
	}

}
