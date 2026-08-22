package curator

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"time"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
)

type Run struct {
	ID               string     `json:"id"`
	Mode             string     `json:"mode"`
	State            string     `json:"state"`
	AnalyzerRevision string     `json:"analyzer_revision"`
	InputSnapshot    []Snapshot `json:"input_snapshot"`
	FindingsCount    int        `json:"findings_count"`
	ProposalsCount   int        `json:"proposals_count"`
	Error            string     `json:"error,omitempty"`
	StartedAt        time.Time  `json:"started_at"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
}

type Snapshot struct {
	SkillID   string `json:"skill_id"`
	VersionID string `json:"version_id"`
	Name      string `json:"name"`
}

type Finding struct {
	ID           string         `json:"id"`
	RunID        string         `json:"run_id"`
	Kind         string         `json:"finding_kind"`
	Severity     string         `json:"severity"`
	LeftSkillID  string         `json:"left_skill_id,omitempty"`
	RightSkillID string         `json:"right_skill_id,omitempty"`
	Score        float64        `json:"score"`
	Evidence     map[string]any `json:"evidence"`
	Proposal     map[string]any `json:"proposal,omitempty"`
	State        string         `json:"state"`
	CreatedAt    time.Time      `json:"created_at"`
}

type Service struct {
	store  *store.Store
	skills *skills.Service
}

func NewService(dataStore *store.Store, skillService *skills.Service) *Service {
	return &Service{store: dataStore, skills: skillService}
}

// RunReportOnly performs deterministic analysis and records its exact input
// versions. It cannot create, mutate, archive, or merge a skill.
func (s *Service) RunReportOnly(ctx context.Context) (Run, error) {
	active, err := s.skills.ListSkills(ctx, false)
	if err != nil {
		return Run{}, err
	}
	snapshot := make([]Snapshot, 0, len(active))
	for _, skill := range active {
		snapshot = append(snapshot, Snapshot{SkillID: skill.ID, VersionID: skill.CurrentVersionID, Name: skill.CanonicalName})
	}
	snapshotJSON, _ := json.Marshal(snapshot)
	now := time.Now().UTC()
	run := Run{ID: identity.New("curator"), Mode: "report_only", State: "running",
		AnalyzerRevision: "curator-deterministic-v2", InputSnapshot: snapshot, StartedAt: now}
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO curator_runs(id,mode,state,analyzer_revision,
		input_snapshot_json,started_at) VALUES(?,?,?,?,?,?)`, run.ID, run.Mode, run.State,
		run.AnalyzerRevision, string(snapshotJSON), formatTime(now))
	if err != nil {
		return Run{}, err
	}
	relations, analyzeErr := s.skills.AnalyzeRelations(ctx)
	persistCtx := context.WithoutCancel(ctx)
	completed := time.Now().UTC()
	if analyzeErr != nil {
		_, _ = s.store.DB.ExecContext(persistCtx, `UPDATE curator_runs SET state='failed', error=?, completed_at=? WHERE id=?`,
			analyzeErr.Error(), formatTime(completed), run.ID)
		return Run{}, analyzeErr
	}
	findings, persistErr := s.buildAndPersistFindings(persistCtx, run.ID, active, relations)
	if persistErr != nil {
		_, _ = s.store.DB.ExecContext(persistCtx, `UPDATE curator_runs SET state='failed', error=?, completed_at=? WHERE id=?`,
			persistErr.Error(), formatTime(completed), run.ID)
		return Run{}, persistErr
	}
	run.State = "completed"
	run.FindingsCount = len(findings)
	for _, finding := range findings {
		if len(finding.Proposal) > 0 {
			run.ProposalsCount++
		}
	}
	run.CompletedAt = &completed
	_, err = s.store.DB.ExecContext(persistCtx, `UPDATE curator_runs SET state=?, findings_count=?,proposals_count=?,
		completed_at=? WHERE id=?`, run.State, run.FindingsCount, run.ProposalsCount, formatTime(completed), run.ID)
	if err != nil {
		return Run{}, err
	}
	return run, nil
}

// ApplyConfiguredAuthority is deliberately separate from RunReportOnly. It
// evaluates only version-bound, high-confidence stale findings against the
// current user-owned authority policy. Manual policy therefore preserves the
// report-only behavior exactly.
func (s *Service) ApplyConfiguredAuthority(ctx context.Context, runID string) ([]skills.AuthorityAction, error) {
	findings, err := s.ListFindings(ctx, runID)
	if err != nil {
		return nil, err
	}
	var actions []skills.AuthorityAction
	for _, finding := range findings {
		if finding.Kind != "stale" || finding.State != "open" || finding.Score < 0.8 {
			continue
		}
		exactVersion, _ := finding.Evidence["exact_version_id"].(string)
		if exactVersion == "" {
			continue
		}
		action, applyErr := s.skills.TryCuratorArchive(ctx, finding.LeftSkillID, finding.ID, exactVersion, finding.Score)
		if applyErr != nil {
			return actions, applyErr
		}
		if action == nil {
			continue
		}
		actions = append(actions, *action)
		if action.State == "completed" {
			_, err = s.store.DB.ExecContext(ctx, `UPDATE curator_findings SET state='automated' WHERE id=? AND state='open'`, finding.ID)
			if err != nil {
				return actions, err
			}
		}
	}
	return actions, nil
}

func (s *Service) buildAndPersistFindings(ctx context.Context, runID string, active []skills.Skill, relations []skills.Relation) ([]Finding, error) {
	findings := []Finding{}
	now := time.Now().UTC()
	for _, relation := range relations {
		severity := "info"
		if relation.Kind == "duplicate" || relation.Kind == "possible_duplicate" {
			severity = "warning"
		}
		proposal := map[string]any{}
		if relation.Kind != "overlap" {
			proposal = map[string]any{
				"action": "consolidate_as_candidate", "automatic_mutation": false,
				"left_version_id": relation.LeftVersionID, "right_version_id": relation.RightVersionID,
				"absorbed_lineage": []string{relation.LeftVersionID, relation.RightVersionID},
				"review_steps":     []string{"inspect bounded diff", "choose canonical owner", "author merged candidate", "run both replay suites", "approve archive separately"},
				"replay_plan":      map[string]any{"required": true, "baseline_versions": []string{relation.LeftVersionID, relation.RightVersionID}, "block_on_regression": true},
			}
		}
		finding := Finding{ID: identity.New("finding"), RunID: runID, Kind: relation.Kind, Severity: severity,
			LeftSkillID: relation.LeftSkillID, RightSkillID: relation.RightSkillID, Score: relation.Score,
			Evidence: relation.Evidence, Proposal: proposal, State: "open", CreatedAt: now}
		if err := s.insertFinding(ctx, finding); err != nil {
			return nil, err
		}
		findings = append(findings, finding)
	}
	for _, skill := range active {
		score, evidence := staleScore(skill, now)
		if score < 0.6 || skill.Pinned || skill.Protected {
			continue
		}
		severity := "info"
		if score >= 0.8 {
			severity = "warning"
		}
		finding := Finding{ID: identity.New("finding"), RunID: runID, Kind: "stale", Severity: severity,
			LeftSkillID: skill.ID, Score: score, Evidence: evidence, Proposal: map[string]any{
				"action": "review_for_archive", "automatic_mutation": false, "exact_version_id": skill.CurrentVersionID,
				"review_steps": []string{"verify replacement or obsolescence", "inspect usage evidence", "archive with explicit actor and reason"},
			}, State: "open", CreatedAt: now}
		if err := s.insertFinding(ctx, finding); err != nil {
			return nil, err
		}
		findings = append(findings, finding)
	}
	return findings, nil
}

func staleScore(skill skills.Skill, now time.Time) (float64, map[string]any) {
	ageDays := now.Sub(skill.UpdatedAt).Hours() / 24
	daysSinceUse := ageDays
	if skill.LastUsedAt != nil {
		daysSinceUse = now.Sub(*skill.LastUsedAt).Hours() / 24
	}
	score := 0.0
	reasons := []string{}
	if skill.LastUsedAt == nil && ageDays >= 90 {
		score += 0.65
		reasons = append(reasons, "never activated in the retained evidence window")
	} else if daysSinceUse >= 90 {
		score += 0.55
		reasons = append(reasons, "not activated for at least 90 days")
	}
	totalOutcomes := skill.SuccessCount + skill.FailureCount
	if totalOutcomes >= 3 && float64(skill.FailureCount)/float64(totalOutcomes) >= 0.75 {
		score += 0.35
		reasons = append(reasons, "observed failure ratio is at least 75 percent")
	}
	if score > 1 {
		score = 1
	}
	score = math.Round(score*1000) / 1000
	return score, map[string]any{"exact_version_id": skill.CurrentVersionID, "age_days": math.Round(ageDays),
		"days_since_use": math.Round(daysSinceUse), "selected": skill.SelectedCount, "injected": skill.InjectedCount,
		"successes": skill.SuccessCount, "failures": skill.FailureCount, "reasons": reasons}
}

func (s *Service) insertFinding(ctx context.Context, finding Finding) error {
	evidence, _ := json.Marshal(finding.Evidence)
	proposal, _ := json.Marshal(finding.Proposal)
	_, err := s.store.DB.ExecContext(ctx, `INSERT INTO curator_findings(id,run_id,finding_kind,severity,left_skill_id,
    right_skill_id,score,evidence_json,proposal_json,state,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, finding.ID,
		finding.RunID, finding.Kind, finding.Severity, nullIfEmpty(finding.LeftSkillID), nullIfEmpty(finding.RightSkillID),
		finding.Score, string(evidence), string(proposal), finding.State, formatTime(finding.CreatedAt))
	return err
}

func (s *Service) ListFindings(ctx context.Context, runID string) ([]Finding, error) {
	query := `SELECT id,run_id,finding_kind,severity,COALESCE(left_skill_id,''),COALESCE(right_skill_id,''),score,
    evidence_json,proposal_json,state,created_at FROM curator_findings`
	args := []any{}
	if runID != "" {
		query += ` WHERE run_id=?`
		args = append(args, runID)
	}
	query += ` ORDER BY CASE severity WHEN 'warning' THEN 0 ELSE 1 END,score DESC,created_at DESC LIMIT 1000`
	rows, err := s.store.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Finding{}
	for rows.Next() {
		var item Finding
		var evidence, proposal, created string
		if err := rows.Scan(&item.ID, &item.RunID, &item.Kind, &item.Severity, &item.LeftSkillID, &item.RightSkillID,
			&item.Score, &evidence, &proposal, &item.State, &created); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(evidence), &item.Evidence)
		_ = json.Unmarshal([]byte(proposal), &item.Proposal)
		item.CreatedAt, _ = parseTime(created)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) ListRuns(ctx context.Context, limit int) ([]Run, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id,mode,state,analyzer_revision,input_snapshot_json,
		findings_count,proposals_count,error,started_at,completed_at FROM curator_runs ORDER BY started_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Run, 0)
	for rows.Next() {
		var run Run
		var snapshotJSON, started string
		var completed sql.NullString
		if err := rows.Scan(&run.ID, &run.Mode, &run.State, &run.AnalyzerRevision, &snapshotJSON,
			&run.FindingsCount, &run.ProposalsCount, &run.Error, &started, &completed); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(snapshotJSON), &run.InputSnapshot)
		run.StartedAt, _ = parseTime(started)
		if completed.Valid {
			value, _ := parseTime(completed.String)
			run.CompletedAt = &value
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func formatTime(value time.Time) string         { return value.UTC().Format(time.RFC3339Nano) }
func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
