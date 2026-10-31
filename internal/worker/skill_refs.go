package worker

import (
	"errors"
	"strings"
	"unicode/utf8"
)

func validateSkillRefs(refs []SkillRef) error {
	if len(refs) > 3 {
		return errors.New("too many skill refs")
	}
	seen := map[string]bool{}
	total := 0
	for _, ref := range refs {
		for _, id := range []string{ref.SkillID, ref.VersionID, ref.Name} {
			if len(strings.TrimSpace(id)) == 0 || len(id) > 200 || !utf8.ValidString(id) {
				return errors.New("identity must be valid UTF-8, nonempty, <=200 bytes")
			}
		}
		if len(strings.TrimSpace(ref.Content)) == 0 || len(ref.Content) > 5000 || !utf8.ValidString(ref.Content) {
			return errors.New("content must be valid UTF-8, nonempty, <=5000 bytes")
		}
		if ref.ContentHash != Hash(ref.Content) {
			return errors.New("content_hash must equal Hash(content)")
		}
		if seen[ref.SkillID] {
			return errors.New("duplicate skill_id")
		}
		seen[ref.SkillID] = true
		total += len(ref.Content)
		if total > 6000 {
			return errors.New("total content exceeds 6000 bytes")
		}
	}
	return nil
}
