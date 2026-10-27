package worker

import "testing"

func TestBoundedFileViewTamperedSnapshot(t *testing.T) {
	originalSource := "abc"
	originalHash := Hash(originalSource)
	view, err := NewBoundedFileView("internal/x.go", originalSource, originalHash, 0, 3)
	if err != nil {
		t.Fatalf("NewBoundedFileView: %v", err)
	}
	if view.RegionID == "" {
		t.Fatal("RegionID must be nonempty")
	}
	if err := ValidateBoundedFileView("internal/x.go", originalSource, originalHash, view); err != nil {
		t.Fatalf("Validate original: %v", err)
	}

	fieldNames := []string{"Path", "FullSHA256", "Content", "RegionID", "Start", "End"}
	for _, name := range fieldNames {
		bad := view
		switch name {
		case "Path":
			bad.Path = "internal/y.go"
		case "FullSHA256":
			bad.FullSHA256 = Hash("different")
		case "Content":
			bad.Content = "abcx"
		case "RegionID":
			bad.RegionID = "tampered"
		case "Start":
			bad.Start = -1
		case "End":
			bad.End = 4
		}
		if err := ValidateBoundedFileView("internal/x.go", originalSource, originalHash, bad); err == nil {
			t.Fatalf("Validate tampered %s: expected error", name)
		}
	}

	if err := ValidateBoundedFileView("internal/y.go", originalSource, originalHash, view); err == nil {
		t.Fatal("Validate other expectedPath: expected error")
	}
	if err := ValidateBoundedFileView("internal/x.go", "abcx", originalHash, view); err == nil {
		t.Fatal("Validate changed source old hash: expected error")
	}
	if err := ValidateBoundedFileView("internal/x.go", "abcx", Hash("abcx"), view); err == nil {
		t.Fatal("Validate changed source new hash: expected error")
	}
}
