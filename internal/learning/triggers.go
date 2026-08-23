package learning

import "strings"

// The trigger vocabulary lives here, next to the policy that uses it, because
// two places asking "did the user correct us?" with two lists would drift into
// two different answers. The agent classifies a turn with these; the reviewer's
// deterministic pre-filter reads the same words.

var explicitLearnMarkers = []string{
	"จำไว้", "เรียนรู้จาก", "สร้างสกิล", "สร้าง skill",
	"remember this", "learn this", "create a skill", "save as a skill",
}

var correctionMarkers = []string{
	"แก้ใหม่", "ไม่ใช่", "ผิด", "ทำซ้ำ",
	"correct that", "that's wrong", "not what i asked", "try again",
}

// ExplicitLearnRequested reports whether the user asked, in so many words, for
// something to be kept.
func ExplicitLearnRequested(value string) bool {
	return containsAny(value, explicitLearnMarkers)
}

// CorrectionRequested reports whether the user was pushing back rather than
// asking something new. A correction is the strongest signal that a procedure
// was missing.
func CorrectionRequested(value string) bool {
	return containsAny(value, correctionMarkers)
}

func containsAny(value string, markers []string) bool {
	value = strings.ToLower(value)
	for _, marker := range markers {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}
