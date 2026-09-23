package worker

import (
	"strings"
	"testing"
)

func TestProjectBrainReferencesRemainBoundedExternalData(t *testing.T) {
	ref := KnowledgeRef{Citation: "project-brain:curated/fix.md:1", Version: "sha256:" + strings.Repeat("a", 64),
		Provenance: "pi-second-brain", Trust: "external_curated_not_verified",
		Content: "Project Brain reference data: an earlier fix changed subtraction to addition."}
	if err := validateKnowledgeRefs([]KnowledgeRef{ref}); err != nil {
		t.Fatalf("valid reference rejected: %v", err)
	}
	for _, refs := range [][]KnowledgeRef{
		{ref, ref},
		{func() KnowledgeRef { changed := ref; changed.Trust = "verified_check"; return changed }()},
		{func() KnowledgeRef { changed := ref; changed.Version = "mutable"; return changed }()},
		{func() KnowledgeRef { changed := ref; changed.Content = strings.Repeat("x", 5001); return changed }()},
	} {
		if err := validateKnowledgeRefs(refs); err == nil {
			t.Fatalf("invalid external reference accepted: %+v", refs)
		}
	}
}
