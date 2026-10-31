// Package taskcoord provides bounded Task Skill selection for taskcoord.
//
// It reuses the shared skills.RankBindings policy so Chat and Task share the
// same rank/scope behavior. Task selection is read-only: it never promotes,
// mutates the active catalog, or grants tools/writes. SkillRefs are advisory
// data only.
package taskcoord

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/worker"
)

// TaskSkillSource is the read-only skill catalog used for Task selection.
// It is intentionally small: list owner-visible skills and read one immutable
// version. No mutation, promotion, or write authority is exposed.
type TaskSkillSource interface {
	// ListSkills returns owner-visible skills. includeArchived controls whether
	// archived rows are included; Task selection always passes false.
	ListSkills(ctx context.Context, includeArchived bool) ([]skills.Skill, error)
	// GetVersion returns one immutable version by ID. The returned Markdown is
	// the exact body used for Task refs; ContentHash is the PACKAGE hash and
	// must not be compared to the Markdown hash.
	GetVersion(ctx context.Context, versionID string) (skills.Version, error)
}

// taskSkillRefs selects bounded, whole, immutable skill bodies for a task.
// It returns nil, nil when skillSource is not wired. It fails before provider
// dispatch on read/identity errors and skips only whole oversized/nonapplicable
// refs. It never truncates a ref.
func (s *Service) taskSkillRefs(ctx context.Context, projectID, query string, profile providers.Profile) ([]worker.SkillRef, error) {
	if s.skillSource == nil {
		return nil, nil
	}
	all, err := s.skillSource.ListSkills(ctx, false)
	if err != nil {
		return nil, err
	}
	var catalog []skills.Binding
	for _, item := range all {
		if !item.Enabled || item.State != skills.StateActive || item.CurrentVersionID == "" {
			continue
		}
		if !skills.AppliesToProject(item, projectID) {
			continue
		}
		catalog = append(catalog, skills.Binding{
			SkillID:       item.ID,
			VersionID:     item.CurrentVersionID,
			CanonicalName: item.CanonicalName,
			Summary:       item.Summary,
			Pinned:        item.Pinned,
		})
	}
	selected := skills.RankBindings(query, catalog, nil)
	var refs []worker.SkillRef
	total := 0
	for _, b := range selected {
		v, err := s.skillSource.GetVersion(ctx, b.VersionID)
		if err != nil {
			return nil, err
		}
		if v.ID != b.VersionID || v.SkillID != b.SkillID {
			return nil, errors.New("version identity mismatch")
		}
		content := v.Markdown
		if len(strings.TrimSpace(content)) == 0 || len(content) > 5000 || !utf8.ValidString(content) {
			continue
		}
		if total+len(content) > 6000 {
			continue
		}
		ref := worker.SkillRef{
			SkillID:     b.SkillID,
			VersionID:   b.VersionID,
			ContentHash: worker.Hash(content),
			Name:        b.CanonicalName,
			Content:     content,
		}
		refs = append(refs, ref)
		total += len(content)
	}
	if len(refs) > 3 {
		refs = refs[:3]
	}
	encoded, err := json.Marshal(refs)
	if err != nil {
		return nil, err
	}
	if s.providers != nil && s.providers.CredentialAppearsIn(profile, string(encoded)) {
		return nil, errors.New("credential detected in skill refs")
	}
	return refs, nil
}

// skillRefIdentities returns lightweight ID/version/hash metadata for the
// selected refs without exposing full contents. It is independent of the
// full refs returned by taskSkillRefs.
func skillRefIdentities(refs []worker.SkillRef) []map[string]string {
	out := make([]map[string]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, map[string]string{
			"skill_id":     r.SkillID,
			"version_id":   r.VersionID,
			"content_hash": r.ContentHash,
			"name":         r.Name,
		})
	}
	return out
}
