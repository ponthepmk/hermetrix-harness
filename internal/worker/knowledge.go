package worker

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var knowledgeVersion = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func validateKnowledgeRefs(refs []KnowledgeRef) error {
	if len(refs) > 3 {
		return fmt.Errorf("Project Brain references exceed the bounded list")
	}
	seen := map[string]bool{}
	total := 0
	for _, ref := range refs {
		if ref.Citation == "" || len(ref.Citation) > 700 || seen[ref.Citation] ||
			!knowledgeVersion.MatchString(ref.Version) || ref.Provenance != "pi-second-brain" ||
			ref.Trust != "external_curated_not_verified" || strings.TrimSpace(ref.Content) == "" ||
			len(ref.Content) > 5000 || !utf8.ValidString(ref.Content) {
			return fmt.Errorf("Project Brain reference identity, trust, or content is invalid")
		}
		seen[ref.Citation] = true
		total += len(ref.Content)
	}
	if total > 6000 {
		return fmt.Errorf("Project Brain reference content exceeds 6 KiB")
	}
	return nil
}
