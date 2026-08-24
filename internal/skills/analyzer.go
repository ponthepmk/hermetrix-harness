package skills

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/textmatch"
)

const deterministicAnalyzerVersion = "deterministic-v1"

type analysisItem struct {
	skill    Skill
	version  Version
	terms    map[string]bool
	triggers map[string]bool
	tools    map[string]bool
}

// AnalyzeRelations performs cheap, deterministic retrieval. It never merges,
// archives, or edits a skill. An optional local-model pair judge can consume
// only these top candidates in a later phase.
func (s *Service) AnalyzeRelations(ctx context.Context) ([]Relation, error) {
	skills, err := s.ListSkills(ctx, false)
	if err != nil {
		return nil, err
	}
	items := make([]analysisItem, 0, len(skills))
	for _, skill := range skills {
		if skill.CurrentVersionID == "" {
			continue
		}
		version, err := s.GetVersion(ctx, skill.CurrentVersionID)
		if err != nil {
			return nil, err
		}
		items = append(items, analysisItem{
			skill: skill, version: version,
			terms:    termSet(version.Manifest.Description + "\n" + stripFrontmatter(version.Markdown)),
			triggers: termSet(version.Manifest.Description),
			tools:    stringSet(version.Manifest.Tools),
		})
	}
	findings := make([]Relation, 0)
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			left, right := items[i], items[j]
			textScore := jaccard(left.terms, right.terms)
			triggerScore := jaccard(left.triggers, right.triggers)
			toolScore := jaccard(left.tools, right.tools)
			score := similarityScore(textScore, triggerScore, toolScore,
				len(left.tools) == 0 && len(right.tools) == 0)
			kind := "unrelated"
			if left.version.ContentHash == right.version.ContentHash {
				kind, score = "duplicate", 1
			} else if score >= possibleDuplicateThreshold {
				kind = "possible_duplicate"
			} else if score >= overlapThreshold {
				kind = "overlap"
			}
			if kind == "unrelated" {
				continue
			}
			evidence := map[string]any{
				"text_jaccard": round(textScore), "trigger_jaccard": round(triggerScore),
				"tool_jaccard": round(toolScore), "shared_terms": shared(left.terms, right.terms, 12),
				"note": "retrieval candidate only; human review is required before consolidation",
			}
			relation := Relation{
				ID: identity.New("rel"), LeftSkillID: left.skill.ID, LeftVersionID: left.version.ID,
				LeftName: left.skill.CanonicalName, RightSkillID: right.skill.ID, RightVersionID: right.version.ID,
				RightName: right.skill.CanonicalName, Kind: kind, Score: round(score), Evidence: evidence,
				AnalyzerKind: "deterministic", AnalyzerVersion: deterministicAnalyzerVersion,
				Status: "open", CreatedAt: time.Now().UTC(),
			}
			if err := s.upsertRelation(ctx, relation); err != nil {
				return nil, err
			}
			findings = append(findings, relation)
		}
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].Score > findings[j].Score })
	return findings, nil
}

func (s *Service) ListRelations(ctx context.Context) ([]Relation, error) {
	rows, err := s.store.DB.QueryContext(ctx, `SELECT r.id, r.left_skill_id, r.left_version_id, ls.canonical_name,
		r.right_skill_id, r.right_version_id, rs.canonical_name, r.relation_kind, r.score,
		r.evidence_json, r.analyzer_kind, r.analyzer_version, r.status, r.created_at
		FROM skill_relations r JOIN skills ls ON ls.id=r.left_skill_id JOIN skills rs ON rs.id=r.right_skill_id
		WHERE ls.current_version_id=r.left_version_id AND rs.current_version_id=r.right_version_id
		ORDER BY r.score DESC, r.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Relation, 0)
	for rows.Next() {
		var relation Relation
		var evidenceJSON, created string
		if err := rows.Scan(&relation.ID, &relation.LeftSkillID, &relation.LeftVersionID, &relation.LeftName,
			&relation.RightSkillID, &relation.RightVersionID, &relation.RightName, &relation.Kind,
			&relation.Score, &evidenceJSON, &relation.AnalyzerKind, &relation.AnalyzerVersion,
			&relation.Status, &created); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(evidenceJSON), &relation.Evidence)
		relation.CreatedAt, _ = parseTime(created)
		out = append(out, relation)
	}
	return out, rows.Err()
}

func (s *Service) upsertRelation(ctx context.Context, relation Relation) error {
	leftVersion, rightVersion := relation.LeftVersionID, relation.RightVersionID
	leftSkill, rightSkill := relation.LeftSkillID, relation.RightSkillID
	if leftVersion > rightVersion {
		leftVersion, rightVersion = rightVersion, leftVersion
		leftSkill, rightSkill = rightSkill, leftSkill
	}
	evidence, _ := json.Marshal(relation.Evidence)
	_, err := s.store.DB.ExecContext(ctx, `INSERT INTO skill_relations(
		id, left_skill_id, left_version_id, right_skill_id, right_version_id, relation_kind,
		score, evidence_json, analyzer_kind, analyzer_version, status, created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(left_version_id, right_version_id, analyzer_kind, analyzer_version)
		DO UPDATE SET relation_kind=excluded.relation_kind, score=excluded.score,
		evidence_json=excluded.evidence_json, status='open', created_at=excluded.created_at`,
		relation.ID, leftSkill, leftVersion, rightSkill, rightVersion, relation.Kind, relation.Score,
		string(evidence), relation.AnalyzerKind, relation.AnalyzerVersion, relation.Status, formatTime(relation.CreatedAt))
	return err
}

func stripFrontmatter(markdown string) string {
	if !strings.HasPrefix(strings.TrimSpace(markdown), "---") {
		return markdown
	}
	parts := strings.SplitN(markdown, "---", 3)
	if len(parts) == 3 {
		return parts[2]
	}
	return markdown
}

// termSet delegates to the shared tokenizer. This file used to carry its own
// copy; the agent carried a different one that was blind to unspaced scripts,
// and the two drifting apart is what made Thai retrieval fail on the path
// users actually touch. One implementation now, used by both.
func termSet(text string) map[string]bool { return textmatch.Union(text) }

func stringSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		if value = strings.ToLower(strings.TrimSpace(value)); value != "" {
			out[value] = true
		}
	}
	return out
}

func jaccard(left, right map[string]bool) float64 {
	if len(left) == 0 && len(right) == 0 {
		return 0
	}
	intersection := 0
	union := len(left)
	for key := range right {
		if left[key] {
			intersection++
		} else {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func shared(left, right map[string]bool, limit int) []string {
	var out []string
	for key := range left {
		if right[key] {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func round(value float64) float64 { return math.Round(value*1000) / 1000 }

// This analyzer is a retrieval stage, not a judge. Its findings are report-only
// and go to a human before anything is consolidated, so it should err towards
// surfacing a pair rather than missing one -- recall matters, precision is the
// reviewer's job.
//
// It was not behaving that way. Measured on four Skills, one pair a deliberate
// paraphrase of another:
//
//	paraphrase pair   0.306
//	every other pair  0.015 - 0.064
//	overlap threshold 0.48
//
// The signal separates cleanly by about five times. The threshold simply sat
// above everything real, so nothing was ever retrieved and the semantic stage
// behind it never received a candidate.
//
// Two causes. Jaccard returns zero for two empty sets, so a pair that both
// declare no tools lost the whole 0.15 tool weight -- absent evidence counted
// as evidence of difference. And term overlap between two procedures written in
// different words runs around a third, nowhere near a threshold set for
// near-verbatim text.
//
// THRESHOLDS ARE CALIBRATED ON ONE MEASURED EXAMPLE. They separate that example
// from its neighbours with room to spare, and they should be revisited against
// a real corpus (P8-A) rather than trusted as tuned.
const (
	overlapThreshold           = 0.25
	possibleDuplicateThreshold = 0.55
)

// similarityScore weights the components that carry signal. When neither Skill
// declares a tool there is nothing to compare, so that weight is redistributed
// across text and trigger instead of scoring as dissimilarity.
func similarityScore(textScore, triggerScore, toolScore float64, toolsAbsent bool) float64 {
	if toolsAbsent {
		return round(0.70*textScore + 0.30*triggerScore)
	}
	return round(0.60*textScore + 0.25*triggerScore + 0.15*toolScore)
}
