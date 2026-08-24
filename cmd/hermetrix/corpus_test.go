package main

import (
	"testing"

	"hermetrix-harness/internal/learning"
)

// TestDigestShapeGroupsEvidenceThatAsksTheSameQuestion is what the corpus
// sampling rests on. A hundred and sixty-two milestone digests collapsed to
// twelve shapes, one of which held a hundred and seven: that is one case
// repeated, and letting it stand would put two thirds of the corpus on a single
// easy negative.
func TestDigestShapeGroupsEvidenceThatAsksTheSameQuestion(t *testing.T) {
	readOnly := learning.Digest{ToolReceipts: []string{"event:a:skill_search:succeeded"}}
	readOnlyAgain := learning.Digest{ToolReceipts: []string{"event:b:skill_search:succeeded"}}
	if digestShape(readOnly) != digestShape(readOnlyAgain) {
		t.Fatal("two searches of the same kind were treated as different evidence")
	}
	wrote := learning.Digest{ToolReceipts: []string{"event:c:workspace.write_file:succeeded"}}
	if digestShape(wrote) == digestShape(readOnly) {
		t.Fatal("work that changed a file has the same shape as work that read one")
	}
	corrected := learning.Digest{ToolReceipts: []string{"event:a:skill_search:succeeded"},
		UserCorrections: []string{"event:x"}}
	if digestShape(corrected) == digestShape(readOnly) {
		t.Fatal("a turn the user corrected has the same shape as one they did not")
	}
	withSkill := learning.Digest{ToolReceipts: []string{"event:a:skill_search:succeeded"},
		SkillActivations: []string{"skill:s@v"}}
	if digestShape(withSkill) == digestShape(readOnly) {
		t.Fatal("a turn with a Skill in play has the same shape as one without")
	}
	// Tool order is not a difference: the same tools in either order ask the
	// reviewer the same question.
	first := learning.Digest{ToolReceipts: []string{
		"event:a:workspace.read_file:succeeded", "event:b:skill_search:succeeded"}}
	second := learning.Digest{ToolReceipts: []string{
		"event:c:skill_search:succeeded", "event:d:workspace.read_file:succeeded"}}
	if digestShape(first) != digestShape(second) {
		t.Fatalf("tool order changed the shape: %q and %q", digestShape(first), digestShape(second))
	}
}
