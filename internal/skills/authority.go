package skills

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"hermetrix-harness/internal/durability"
	"hermetrix-harness/internal/identity"
)

const (
	AuthorityManual = "manual"
	AuthorityGated  = "gated_automation"
)

type AuthorityPolicy struct {
	Mode                    string    `json:"mode"`
	AutoPromoteAgentCreate  bool      `json:"auto_promote_agent_create"`
	AutoPromoteAgentImprove bool      `json:"auto_promote_agent_improve"`
	AutoArchiveAgentSkills  bool      `json:"auto_archive_agent_skills"`
	AllowedScopes           []string  `json:"allowed_scopes"`
	MaxCandidateTokens      int       `json:"max_candidate_tokens"`
	Revision                int       `json:"revision"`
	UpdatedBy               string    `json:"updated_by"`
	UpdateReason            string    `json:"update_reason"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type SaveAuthorityPolicyInput struct {
	Mode                    string   `json:"mode"`
	AutoPromoteAgentCreate  bool     `json:"auto_promote_agent_create"`
	AutoPromoteAgentImprove bool     `json:"auto_promote_agent_improve"`
	AutoArchiveAgentSkills  bool     `json:"auto_archive_agent_skills"`
	AllowedScopes           []string `json:"allowed_scopes"`
	MaxCandidateTokens      int      `json:"max_candidate_tokens"`
	Actor                   string   `json:"actor"`
	Reason                  string   `json:"reason"`
	ExpectedRevision        int      `json:"expected_revision"`
}

type AuthorityAction struct {
	ID                  string     `json:"id"`
	ActionKind          string     `json:"action_kind"`
	CandidateID         string     `json:"candidate_id,omitempty"`
	SkillID             string     `json:"skill_id,omitempty"`
	BeforeVersionID     string     `json:"before_version_id,omitempty"`
	AfterVersionID      string     `json:"after_version_id,omitempty"`
	PolicyRevision      int        `json:"policy_revision"`
	Actor               string     `json:"actor"`
	State               string     `json:"state"`
	Reason              string     `json:"reason,omitempty"`
	Error               string     `json:"error,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	CompletedAt         *time.Time `json:"completed_at,omitempty"`
	RollbackCandidateID string     `json:"rollback_candidate_id,omitempty"`
	ArchiveID           string     `json:"archive_id,omitempty"`
}

func (s *Service) GetAuthorityPolicy(ctx context.Context) (AuthorityPolicy, error) {
	var policy AuthorityPolicy
	var scopesJSON, created, updated string
	err := s.store.DB.QueryRowContext(ctx, `SELECT mode,auto_promote_agent_create,auto_promote_agent_improve,
		auto_archive_agent_skills,allowed_scopes_json,max_candidate_tokens,revision,updated_by,update_reason,created_at,updated_at
		FROM skill_authority_policy WHERE id='local'`).Scan(&policy.Mode, &policy.AutoPromoteAgentCreate,
		&policy.AutoPromoteAgentImprove, &policy.AutoArchiveAgentSkills, &scopesJSON, &policy.MaxCandidateTokens,
		&policy.Revision, &policy.UpdatedBy, &policy.UpdateReason, &created, &updated)
	if err != nil {
		return AuthorityPolicy{}, err
	}
	_ = json.Unmarshal([]byte(scopesJSON), &policy.AllowedScopes)
	policy.CreatedAt, _ = parseTime(created)
	policy.UpdatedAt, _ = parseTime(updated)
	return policy, nil
}

func (s *Service) SaveAuthorityPolicy(ctx context.Context, input SaveAuthorityPolicyInput) (AuthorityPolicy, error) {
	input.Mode = strings.TrimSpace(input.Mode)
	input.Actor = strings.TrimSpace(input.Actor)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Mode != AuthorityManual && input.Mode != AuthorityGated {
		return AuthorityPolicy{}, fmt.Errorf("authority mode must be manual or gated_automation")
	}
	if input.Actor == "" || input.Reason == "" || utf8.RuneCountInString(input.Actor) > 120 || utf8.RuneCountInString(input.Reason) > 1000 {
		return AuthorityPolicy{}, fmt.Errorf("authority policy requires a bounded actor and reason")
	}
	if input.MaxCandidateTokens < 256 || input.MaxCandidateTokens > 16384 {
		return AuthorityPolicy{}, fmt.Errorf("candidate token ceiling must be between 256 and 16384")
	}
	scopes, err := validateAuthorityScopes(input.AllowedScopes)
	if err != nil {
		return AuthorityPolicy{}, err
	}
	if input.Mode == AuthorityManual {
		input.AutoPromoteAgentCreate = false
		input.AutoPromoteAgentImprove = false
		input.AutoArchiveAgentSkills = false
	}
	encodedScopes, _ := json.Marshal(scopes)
	now := time.Now().UTC()
	result, err := s.store.DB.ExecContext(ctx, `UPDATE skill_authority_policy SET mode=?,auto_promote_agent_create=?,
		auto_promote_agent_improve=?,auto_archive_agent_skills=?,allowed_scopes_json=?,max_candidate_tokens=?,
		revision=revision+1,updated_by=?,update_reason=?,updated_at=? WHERE id='local' AND revision=?`, input.Mode,
		boolInt(input.AutoPromoteAgentCreate), boolInt(input.AutoPromoteAgentImprove), boolInt(input.AutoArchiveAgentSkills),
		string(encodedScopes), input.MaxCandidateTokens, input.Actor, input.Reason, formatTime(now), input.ExpectedRevision)
	if err != nil {
		return AuthorityPolicy{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return AuthorityPolicy{}, ErrRevisionConflict
	}
	return s.GetAuthorityPolicy(ctx)
}

func validateAuthorityScopes(values []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "user" && value != "workspace" && value != "agent" {
			return nil, fmt.Errorf("unsupported automated Skill scope %q", value)
		}
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one automated Skill scope is required")
	}
	return out, nil
}

func (s *Service) ProcessPendingAuthority(ctx context.Context) ([]AuthorityAction, error) {
	candidates, err := s.ListCandidates(ctx, CandidateNeedsReview)
	if err != nil {
		return nil, err
	}
	var actions []AuthorityAction
	for _, candidate := range candidates {
		action, err := s.TryAutomatedPromotion(ctx, candidate.ID)
		if err != nil {
			return actions, err
		}
		if action != nil {
			actions = append(actions, *action)
		}
	}
	return actions, nil
}

func (s *Service) TryAutomatedPromotion(ctx context.Context, candidateID string) (*AuthorityAction, error) {
	policy, err := s.GetAuthorityPolicy(ctx)
	if err != nil {
		return nil, err
	}
	if policy.Mode != AuthorityGated {
		return nil, nil
	}
	candidate, err := s.GetCandidate(ctx, candidateID)
	if err != nil {
		return nil, err
	}
	if !trustedAutomationActor(candidate.CreatedBy) || !scopeAllowed(policy.AllowedScopes, candidate.ScopeKind) {
		return nil, nil
	}
	if candidate.ChangeKind == "create" && !policy.AutoPromoteAgentCreate {
		return nil, nil
	}
	if candidate.ChangeKind == "improve" && !policy.AutoPromoteAgentImprove {
		return nil, nil
	}
	if candidate.ChangeKind != "create" && candidate.ChangeKind != "improve" {
		return nil, nil
	}
	if !candidate.Checks.Passed || candidate.Checks.TokenEstimate > policy.MaxCandidateTokens ||
		len(candidate.Checks.CapabilityHints) > 0 || (candidate.Checks.ReplayRequired && !candidate.Checks.ReplayPassed) {
		return nil, nil
	}
	beforeVersion := ""
	if candidate.TargetSkillID != "" {
		target, err := s.GetSkill(ctx, candidate.TargetSkillID)
		if err != nil {
			return nil, err
		}
		if target.Protected || target.Origin == "bundled" || target.Origin == "imported" {
			return nil, nil
		}
		beforeVersion = target.CurrentVersionID
	}
	action := AuthorityAction{ID: identity.New("authority"), ActionKind: "auto_promote", CandidateID: candidate.ID,
		SkillID: candidate.TargetSkillID, BeforeVersionID: beforeVersion, PolicyRevision: policy.Revision,
		Actor: fmt.Sprintf("skill-authority-policy:%d", policy.Revision), State: "running",
		Reason: "candidate satisfied user-configured gated automation", CreatedAt: time.Now().UTC()}
	if err := s.insertAuthorityAction(ctx, action); err != nil {
		return nil, err
	}
	promoted, promoteErr := s.PromoteCandidate(ctx, candidate.ID, action.Actor, candidate.Revision)
	completed := time.Now().UTC()
	if promoteErr != nil {
		action.State, action.Error, action.CompletedAt = "failed", promoteErr.Error(), &completed
		durability.Exec("mark Skill promotion authority action failed").Observe(s.store.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE skill_authority_actions SET state='failed',error=?,completed_at=? WHERE id=?`,
			action.Error, formatTime(completed), action.ID))
		return &action, promoteErr
	}
	action.SkillID, action.AfterVersionID, action.State, action.CompletedAt = promoted.ID, promoted.CurrentVersionID, "completed", &completed
	_, err = s.store.DB.ExecContext(ctx, `UPDATE skill_authority_actions SET skill_id=?,after_version_id=?,state='completed',completed_at=?
		WHERE id=? AND state='running'`, promoted.ID, promoted.CurrentVersionID, formatTime(completed), action.ID)
	return &action, err
}

// TryCuratorArchive applies the curator branch of the user-configured
// authority policy. The caller must provide the exact version from a stale
// finding snapshot; a later Skill change makes the decision ineligible.
func (s *Service) TryCuratorArchive(ctx context.Context, skillID, findingID, expectedVersionID string, staleScore float64) (*AuthorityAction, error) {
	policy, err := s.GetAuthorityPolicy(ctx)
	if err != nil {
		return nil, err
	}
	if policy.Mode != AuthorityGated || !policy.AutoArchiveAgentSkills || staleScore < 0.8 {
		return nil, nil
	}
	skill, err := s.GetSkill(ctx, skillID)
	if err != nil {
		return nil, err
	}
	if skill.CurrentVersionID != expectedVersionID || skill.State == StateArchived || skill.Pinned || skill.Protected ||
		skill.Origin != "agent_promoted" || !scopeAllowed(policy.AllowedScopes, skill.ScopeKind) {
		return nil, nil
	}
	action := AuthorityAction{ID: identity.New("authority"), ActionKind: "auto_archive", SkillID: skill.ID,
		BeforeVersionID: skill.CurrentVersionID, PolicyRevision: policy.Revision,
		Actor: fmt.Sprintf("curator:skill-authority-policy:%d", policy.Revision), State: "running",
		Reason: "high-confidence stale agent Skill from finding " + findingID, CreatedAt: time.Now().UTC()}
	if err := s.insertAuthorityAction(ctx, action); err != nil {
		return nil, err
	}
	archive, archiveErr := s.ArchiveSkill(ctx, skill.ID, action.Actor, action.Reason, "")
	completed := time.Now().UTC()
	if archiveErr != nil {
		action.State, action.Error, action.CompletedAt = "failed", archiveErr.Error(), &completed
		durability.Exec("mark Skill archive authority action failed").Observe(s.store.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE skill_authority_actions
			SET state='failed',error=?,completed_at=? WHERE id=?`, action.Error, formatTime(completed), action.ID))
		return &action, archiveErr
	}
	action.ArchiveID, action.State, action.CompletedAt = archive.ID, "completed", &completed
	_, err = s.store.DB.ExecContext(ctx, `UPDATE skill_authority_actions SET archive_id=?,state='completed',completed_at=?
		WHERE id=? AND state='running'`, archive.ID, formatTime(completed), action.ID)
	return &action, err
}

func trustedAutomationActor(actor string) bool {
	actor = strings.ToLower(strings.TrimSpace(actor))
	return actor == "background_reviewer" || actor == "agent" || strings.HasPrefix(actor, "agent:") || strings.HasPrefix(actor, "curator:")
}

func scopeAllowed(allowed []string, scope string) bool {
	for _, item := range allowed {
		if item == scope {
			return true
		}
	}
	return false
}

func (s *Service) insertAuthorityAction(ctx context.Context, action AuthorityAction) error {
	_, err := s.store.DB.ExecContext(ctx, `INSERT INTO skill_authority_actions(id,action_kind,candidate_id,skill_id,
		before_version_id,after_version_id,policy_revision,actor,state,reason,error,created_at,completed_at,rollback_candidate_id,archive_id)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, action.ID, action.ActionKind, nullIfEmpty(action.CandidateID), nullIfEmpty(action.SkillID),
		action.BeforeVersionID, action.AfterVersionID, action.PolicyRevision, action.Actor, action.State, action.Reason,
		action.Error, formatTime(action.CreatedAt), nullTime(action.CompletedAt), action.RollbackCandidateID, action.ArchiveID)
	return err
}

func (s *Service) ListAuthorityActions(ctx context.Context, limit int) ([]AuthorityAction, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id,action_kind,COALESCE(candidate_id,''),COALESCE(skill_id,''),
		before_version_id,after_version_id,policy_revision,actor,state,reason,error,created_at,completed_at,rollback_candidate_id,archive_id
		FROM skill_authority_actions ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// Return a non-nil slice so the JSON body is [] not null on an empty table:
	// the control center assigns this straight onto state and reads .length.
	actions := make([]AuthorityAction, 0)
	for rows.Next() {
		action, err := scanAuthorityAction(rows)
		if err != nil {
			return nil, err
		}
		actions = append(actions, action)
	}
	return actions, rows.Err()
}

func (s *Service) CreateAuthorityRollback(ctx context.Context, actionID, actor, reason string) (Candidate, error) {
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	if actor == "" || reason == "" {
		return Candidate{}, fmt.Errorf("rollback actor and reason are required")
	}
	action, err := scanAuthorityAction(s.store.DB.QueryRowContext(ctx, `SELECT id,action_kind,COALESCE(candidate_id,''),
		COALESCE(skill_id,''),before_version_id,after_version_id,policy_revision,actor,state,reason,error,created_at,
		completed_at,rollback_candidate_id,archive_id FROM skill_authority_actions WHERE id=?`, actionID))
	if errors.Is(err, sql.ErrNoRows) {
		return Candidate{}, ErrNotFound
	}
	if err != nil {
		return Candidate{}, err
	}
	if action.State != "completed" || action.RollbackCandidateID != "" {
		return Candidate{}, fmt.Errorf("authority action is not eligible for rollback")
	}
	if action.ActionKind == "auto_archive" {
		candidate, restoreErr := s.RestoreArchive(ctx, action.ArchiveID, actor, reason)
		if restoreErr != nil {
			return Candidate{}, restoreErr
		}
		_, err = s.store.DB.ExecContext(ctx, `UPDATE skill_authority_actions SET rollback_candidate_id=?
			WHERE id=? AND rollback_candidate_id=''`, candidate.ID, action.ID)
		return candidate, err
	}
	if action.BeforeVersionID == "" {
		if _, err := s.ArchiveSkill(ctx, action.SkillID, actor, "rollback auto-created Skill: "+reason, ""); err != nil {
			return Candidate{}, err
		}
		now := time.Now().UTC()
		_, err := s.store.DB.ExecContext(ctx, `UPDATE skill_authority_actions SET state='rolled_back',completed_at=? WHERE id=? AND state='completed'`,
			formatTime(now), action.ID)
		return Candidate{}, err
	}
	skill, err := s.GetSkill(ctx, action.SkillID)
	if err != nil {
		return Candidate{}, err
	}
	version, err := s.GetVersion(ctx, action.BeforeVersionID)
	if err != nil {
		return Candidate{}, err
	}
	candidate, err := s.CreateCandidate(ctx, CreateCandidateInput{CanonicalName: skill.CanonicalName, ScopeKind: skill.ScopeKind,
		ScopeRef: skill.ScopeRef, Origin: skill.Origin, Owner: skill.Owner, ChangeKind: "improve", TargetSkillID: skill.ID,
		BaseVersionID: skill.CurrentVersionID, CreatedBy: actor, TriggerKind: "authority_rollback", Reason: reason,
		EvidenceRefs: []string{"authority_action:" + action.ID, "skill_version:" + action.BeforeVersionID}, Markdown: version.Markdown})
	if err != nil {
		return Candidate{}, err
	}
	_, err = s.store.DB.ExecContext(ctx, `UPDATE skill_authority_actions SET rollback_candidate_id=? WHERE id=? AND rollback_candidate_id=''`,
		candidate.ID, action.ID)
	return candidate, err
}

func scanAuthorityAction(row scanner) (AuthorityAction, error) {
	var action AuthorityAction
	var created string
	var completed sql.NullString
	if err := row.Scan(&action.ID, &action.ActionKind, &action.CandidateID, &action.SkillID, &action.BeforeVersionID,
		&action.AfterVersionID, &action.PolicyRevision, &action.Actor, &action.State, &action.Reason, &action.Error,
		&created, &completed, &action.RollbackCandidateID, &action.ArchiveID); err != nil {
		return AuthorityAction{}, err
	}
	action.CreatedAt, _ = parseTime(created)
	if completed.Valid {
		value, _ := parseTime(completed.String)
		action.CompletedAt = &value
	}
	return action, nil
}

func nullTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(*value)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
