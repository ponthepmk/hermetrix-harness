package worker

import (
	"testing"
)

func TestBoundedFileViewValidExisting(t *testing.T) {
	source := "abc"
	path := "internal/x.go"
	expectedHash := Hash(source)
	view, err := NewBoundedFileView(path, source, expectedHash, 0, len(source))
	if err != nil {
		t.Fatalf("NewBoundedFileView: %v", err)
	}
	if view.Path != path || view.FullSHA256 != expectedHash || view.Start != 0 || view.End != len(source) || view.Content != source {
		t.Fatalf("view mismatch: %+v", view)
	}
	if view.RegionID == "" {
		t.Fatal("empty RegionID")
	}
	if err := ValidateBoundedFileView(path, source, expectedHash, view); err != nil {
		t.Fatalf("ValidateBoundedFileView: %v", err)
	}
}

func TestBoundedFileViewEmptyVersusAbsent(t *testing.T) {
	view, err := NewBoundedFileView("empty.go", "", Hash(""), 0, 0)
	if err != nil {
		t.Fatalf("empty view: %v", err)
	}
	if view.Content != "" || view.RegionID == "" {
		t.Fatalf("empty view mismatch: %+v", view)
	}
	if err := ValidateBoundedFileView("empty.go", "", Hash(""), view); err != nil {
		t.Fatalf("empty view validate: %v", err)
	}
	if _, err := NewBoundedFileView("empty.go", "", "absent", 0, 0); err == nil {
		t.Fatal("absent hash accepted")
	}
}
