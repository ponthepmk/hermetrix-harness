package skills

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/store"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrRevisionConflict  = errors.New("revision conflict")
	ErrProtectedSkill    = errors.New("protected skill cannot be changed")
	ErrChecksFailed      = errors.New("candidate checks have not passed")
	ErrCandidateNotReady = errors.New("candidate is not awaiting review")
	ErrImmutableMetadata = errors.New("existing skill metadata is immutable in a content improvement")
	ErrForkRequired      = errors.New("imported or bundled skills must be forked rather than mutated")
	ErrReplayRequired    = errors.New("candidate replay evidence is missing or stale")
	ErrCapabilityReview  = errors.New("capability widening requires explicit review")
)

type Service struct{ store *store.Store }

func NewService(s *store.Store) *Service { return &Service{store: s} }

func (s *Service) CreateCandidate(ctx context.Context, in CreateCandidateInput) (Candidate, error) {
	candidate, err := s.prepareCandidate(in)
	if err != nil {
		return Candidate{}, err
	}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return Candidate{}, err
	}
	defer tx.Rollback()
	if err := s.insertCandidate(ctx, tx, candidate); err != nil {
		return Candidate{}, err
	}
	if err := s.appendEvent(ctx, tx, eventInput{CandidateID: candidate.ID, Kind: "candidate_created", ActorKind: candidate.CreatedBy, Payload: map[string]any{"state": candidate.State, "trigger": candidate.TriggerKind}}); err != nil {
		return Candidate{}, err
	}
	if err := tx.Commit(); err != nil {
		return Candidate{}, fmt.Errorf("commit candidate: %w", err)
	}
	if candidate.ChangeKind == "improve" {
		_, _ = s.RunCandidateReplay(ctx, candidate.ID)
		return s.GetCandidate(ctx, candidate.ID)
	}
	return candidate, nil
}

func (s *Service) prepareCandidate(in CreateCandidateInput) (Candidate, error) {
	in.CanonicalName = strings.TrimSpace(in.CanonicalName)
	if in.ScopeKind == "" {
		in.ScopeKind = "user"
	}
	if in.Origin == "" {
		in.Origin = "user_created"
	}
	if in.Owner == "" {
		in.Owner = "user"
	}
	if in.ChangeKind == "" {
		in.ChangeKind = "create"
	}
	if in.CreatedBy == "" {
		in.CreatedBy = "user"
	}
	if in.TriggerKind == "" {
		in.TriggerKind = "manual"
	}
	pkg, err := NewPackage(in.Markdown, in.Files)
	if err != nil {
		return Candidate{}, err
	}
	encoded, err := pkg.Encode()
	if err != nil {
		return Candidate{}, err
	}
	ref, err := s.store.Blobs.Put(encoded)
	if err != nil {
		return Candidate{}, err
	}
	checks := CheckPackage(in.CanonicalName, pkg)
	checks = requireReplayForChange(in.ChangeKind, checks)
	state := CandidateNeedsReview
	if !checks.Passed {
		state = CandidateQuarantined
	}
	now := time.Now().UTC()
	candidate := Candidate{
		ID: identity.New("cand"), CanonicalName: in.CanonicalName, ScopeKind: in.ScopeKind,
		ScopeRef: in.ScopeRef, Origin: in.Origin, Owner: in.Owner, ChangeKind: in.ChangeKind,
		TargetSkillID: in.TargetSkillID, BaseVersionID: in.BaseVersionID,
		CandidateBlobRef: ref, CandidateHash: ref, CreatedBy: in.CreatedBy,
		TriggerKind: in.TriggerKind, Reason: strings.TrimSpace(in.Reason), EvidenceRefs: cleanStrings(in.EvidenceRefs),
		State: state, Checks: checks, Revision: 1, CreatedAt: now, UpdatedAt: now, Markdown: pkg.Markdown(),
		SourceReviewID: in.SourceReviewID,
	}
	return candidate, nil
}

func (s *Service) insertCandidate(ctx context.Context, tx *sql.Tx, candidate Candidate) error {
	evidenceJSON, _ := json.Marshal(candidate.EvidenceRefs)
	checksJSON, _ := json.Marshal(candidate.Checks)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO skill_candidates(
		 id, canonical_name, scope_kind, scope_ref, origin, owner, change_kind,
		 target_skill_id, base_version_id, candidate_blob_ref, candidate_hash,
		 created_by, trigger_kind, reason, evidence_json, state, checks_json,
		 revision, created_at, updated_at, source_review_id)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		candidate.ID, candidate.CanonicalName, candidate.ScopeKind, candidate.ScopeRef,
		candidate.Origin, candidate.Owner, candidate.ChangeKind, nullIfEmpty(candidate.TargetSkillID),
		nullIfEmpty(candidate.BaseVersionID), candidate.CandidateBlobRef, candidate.CandidateHash,
		candidate.CreatedBy, candidate.TriggerKind, candidate.Reason, string(evidenceJSON), candidate.State,
		string(checksJSON), candidate.Revision, formatTime(candidate.CreatedAt), formatTime(candidate.UpdatedAt), nullIfEmpty(candidate.SourceReviewID))
	if err != nil {
		return fmt.Errorf("insert candidate: %w", err)
	}
	return nil
}

func (s *Service) ListCandidates(ctx context.Context, state string) ([]Candidate, error) {
	query := candidateSelect
	args := []any{}
	if state != "" {
		query += ` WHERE state = ?`
		args = append(args, state)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := s.store.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list candidates: %w", err)
	}
	defer rows.Close()
	out := make([]Candidate, 0)
	for rows.Next() {
		c, err := scanCandidate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) GetCandidate(ctx context.Context, id string) (Candidate, error) {
	c, err := scanCandidate(s.store.DB.QueryRowContext(ctx, candidateSelect+` WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Candidate{}, ErrNotFound
	}
	if err != nil {
		return Candidate{}, err
	}
	pkg, err := s.packageByRef(c.CandidateBlobRef)
	if err != nil {
		return Candidate{}, err
	}
	c.Markdown = pkg.Markdown()
	return c, nil
}

func (s *Service) GetCandidateBySourceReview(ctx context.Context, reviewID string) (Candidate, error) {
	if strings.TrimSpace(reviewID) == "" {
		return Candidate{}, ErrNotFound
	}
	c, err := scanCandidate(s.store.DB.QueryRowContext(ctx, candidateSelect+` WHERE source_review_id = ?`, reviewID))
	if errors.Is(err, sql.ErrNoRows) {
		return Candidate{}, ErrNotFound
	}
	if err != nil {
		return Candidate{}, err
	}
	pkg, err := s.packageByRef(c.CandidateBlobRef)
	if err != nil {
		return Candidate{}, err
	}
	c.Markdown = pkg.Markdown()
	return c, nil
}

func (s *Service) UpdateCandidate(ctx context.Context, id string, in UpdateCandidateInput) (Candidate, error) {
	if strings.TrimSpace(in.Actor) == "" {
		return Candidate{}, fmt.Errorf("editor actor is required")
	}
	current, err := s.GetCandidate(ctx, id)
	if err != nil {
		return Candidate{}, err
	}
	if current.State != CandidateNeedsReview && current.State != CandidateQuarantined {
		return Candidate{}, ErrCandidateNotReady
	}
	if in.ExpectedRevision != 0 && in.ExpectedRevision != current.Revision {
		return Candidate{}, ErrRevisionConflict
	}
	oldPackage, err := s.packageByRef(current.CandidateBlobRef)
	if err != nil {
		return Candidate{}, err
	}
	pkg, err := NewPackage(in.Markdown, supportingFiles(oldPackage))
	if err != nil {
		return Candidate{}, err
	}
	encoded, err := pkg.Encode()
	if err != nil {
		return Candidate{}, err
	}
	ref, err := s.store.Blobs.Put(encoded)
	if err != nil {
		return Candidate{}, err
	}
	checks := CheckPackage(current.CanonicalName, pkg)
	checks = requireReplayForChange(current.ChangeKind, checks)
	state := CandidateNeedsReview
	if !checks.Passed {
		state = CandidateQuarantined
	}
	checksJSON, _ := json.Marshal(checks)
	now := time.Now().UTC()
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return Candidate{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE skill_candidates SET candidate_blob_ref=?, candidate_hash=?, state=?,
		checks_json=?, revision=revision+1, updated_at=? WHERE id=? AND revision=? AND state IN (?,?)`,
		ref, ref, state, string(checksJSON), formatTime(now), id, current.Revision,
		CandidateNeedsReview, CandidateQuarantined)
	if err != nil {
		return Candidate{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return Candidate{}, ErrRevisionConflict
	}
	if err := s.appendEvent(ctx, tx, eventInput{CandidateID: id, Kind: "candidate_edited", ActorKind: in.Actor,
		Payload: map[string]any{"before_hash": current.CandidateHash, "after_hash": ref, "checks_passed": checks.Passed}}); err != nil {
		return Candidate{}, err
	}
	if err := tx.Commit(); err != nil {
		return Candidate{}, err
	}
	if current.ChangeKind == "improve" {
		_, _ = s.RunCandidateReplay(ctx, id)
	}
	return s.GetCandidate(ctx, id)
}

func (s *Service) ProposeImprovement(ctx context.Context, skillID, actor, reason string) (Candidate, error) {
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" {
		return Candidate{}, fmt.Errorf("actor and improvement reason are required")
	}
	skill, err := s.GetSkill(ctx, skillID)
	if err != nil {
		return Candidate{}, err
	}
	if skill.State == StateArchived || skill.CurrentVersionID == "" {
		return Candidate{}, fmt.Errorf("only an active or stale skill can be improved")
	}
	if skill.Protected {
		return Candidate{}, ErrProtectedSkill
	}
	version, err := s.GetVersion(ctx, skill.CurrentVersionID)
	if err != nil {
		return Candidate{}, err
	}
	pkg, err := s.packageByRef(version.PackageBlobRef)
	if err != nil {
		return Candidate{}, err
	}
	return s.CreateCandidate(ctx, CreateCandidateInput{CanonicalName: skill.CanonicalName,
		ScopeKind: skill.ScopeKind, ScopeRef: skill.ScopeRef, Origin: skill.Origin, Owner: skill.Owner,
		ChangeKind: "improve", TargetSkillID: skill.ID, BaseVersionID: skill.CurrentVersionID,
		CreatedBy: actor, TriggerKind: "manual_improvement", Reason: reason,
		EvidenceRefs: []string{"skill_version:" + skill.CurrentVersionID}, Markdown: pkg.Markdown(), Files: supportingFiles(pkg)})
}

func (s *Service) ForkSkill(ctx context.Context, skillID, canonicalName, actor, reason string) (Candidate, error) {
	canonicalName, actor, reason = strings.TrimSpace(canonicalName), strings.TrimSpace(actor), strings.TrimSpace(reason)
	if canonicalName == "" || actor == "" || reason == "" {
		return Candidate{}, fmt.Errorf("fork requires a new name, actor and reason")
	}
	skill, err := s.GetSkill(ctx, skillID)
	if err != nil {
		return Candidate{}, err
	}
	if skill.CurrentVersionID == "" {
		return Candidate{}, fmt.Errorf("Skill has no version to fork")
	}
	version, err := s.GetVersion(ctx, skill.CurrentVersionID)
	if err != nil {
		return Candidate{}, err
	}
	pkg, err := s.packageByRef(version.PackageBlobRef)
	if err != nil {
		return Candidate{}, err
	}
	markdown := replaceManifestName(pkg.Markdown(), canonicalName)
	return s.CreateCandidate(ctx, CreateCandidateInput{CanonicalName: canonicalName, ScopeKind: "user",
		Origin: "user_created", Owner: "user", ChangeKind: "create", CreatedBy: actor, TriggerKind: "manual_fork",
		Reason: reason, EvidenceRefs: []string{"skill:" + skill.ID, "skill_version:" + version.ID},
		Markdown: markdown, Files: supportingFiles(pkg)})
}

func replaceManifestName(markdown, name string) string {
	lines := strings.Split(markdown, "\n")
	inFrontmatter := false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if index == 0 && trimmed == "---" {
			inFrontmatter = true
			continue
		}
		if inFrontmatter && trimmed == "---" {
			break
		}
		if inFrontmatter && strings.HasPrefix(trimmed, "name:") {
			lines[index] = "name: " + name
			return strings.Join(lines, "\n")
		}
	}
	return markdown
}

func (s *Service) PromoteCandidate(ctx context.Context, id, actor string, expectedRevision int) (Skill, error) {
	if actor == "" {
		return Skill{}, fmt.Errorf("approval actor is required")
	}
	preflight, err := s.GetCandidate(ctx, id)
	if err != nil {
		return Skill{}, err
	}
	if preflight.State != CandidateNeedsReview {
		return Skill{}, ErrCandidateNotReady
	}
	if expectedRevision != 0 && preflight.Revision != expectedRevision {
		return Skill{}, ErrRevisionConflict
	}
	if !preflight.Checks.Passed {
		return Skill{}, ErrChecksFailed
	}
	preflightPackage, err := s.packageByRef(preflight.CandidateBlobRef)
	if err != nil {
		return Skill{}, err
	}
	if preflight.Checks.ReplayRequired {
		if err := s.requireCurrentReplay(ctx, preflight); err != nil {
			return Skill{}, err
		}
	}
	if err := s.requireCapabilityReview(ctx, preflight, preflightPackage); err != nil {
		return Skill{}, err
	}
	// Last of the gates, and deliberately so. Replay compares lexical fixtures,
	// which catches a Skill whose wording drifted rather than one whose
	// procedure got worse; this asks the second question. It runs after the
	// authority checks because a candidate that widens rights without review
	// must be refused for that reason whatever its evaluation says, and the
	// operator should be told the reason they can act on.
	//
	// Bound to the same change kind as replay: an evaluation against the version
	// being replaced only means something when there is a version being replaced.
	if preflight.Checks.ReplayRequired {
		if err := s.requireCurrentBehavioralEval(ctx, preflight); err != nil {
			return Skill{}, err
		}
	}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return Skill{}, err
	}
	defer tx.Rollback()
	c, err := scanCandidate(tx.QueryRowContext(ctx, candidateSelect+` WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Skill{}, ErrNotFound
	}
	if err != nil {
		return Skill{}, err
	}
	if c.State != CandidateNeedsReview {
		return Skill{}, ErrCandidateNotReady
	}
	if expectedRevision != 0 && c.Revision != expectedRevision {
		return Skill{}, ErrRevisionConflict
	}
	if !c.Checks.Passed {
		return Skill{}, ErrChecksFailed
	}
	pkg, err := s.packageByRef(c.CandidateBlobRef)
	if err != nil {
		return Skill{}, err
	}
	manifest := ParseManifest(pkg.Markdown())
	manifestJSON, _ := json.Marshal(manifest)
	now := time.Now().UTC()
	skillID := c.TargetSkillID
	parentVersion := c.BaseVersionID
	if skillID == "" {
		skillID = identity.New("skill")
		activeOrigin := c.Origin
		if activeOrigin == "agent_candidate" {
			activeOrigin = "agent_promoted"
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO skills(
			id, canonical_name, scope_kind, scope_ref, origin, owner, state,
			enabled, pinned, protected, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?,1,0,0,?,?)`, skillID, c.CanonicalName, c.ScopeKind,
			c.ScopeRef, activeOrigin, c.Owner, StateActive, formatTime(now), formatTime(now))
		if err != nil {
			return Skill{}, fmt.Errorf("create skill: %w", err)
		}
	} else {
		var current sql.NullString
		var protected bool
		var name, scopeKind, scopeRef, origin, owner string
		if err := tx.QueryRowContext(ctx, `SELECT current_version_id, protected, canonical_name, scope_kind,
			scope_ref, origin, owner FROM skills WHERE id = ?`, skillID).
			Scan(&current, &protected, &name, &scopeKind, &scopeRef, &origin, &owner); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Skill{}, ErrNotFound
			}
			return Skill{}, err
		}
		if protected {
			return Skill{}, ErrProtectedSkill
		}
		if origin == "bundled" || origin == "imported" {
			return Skill{}, ErrForkRequired
		}
		if current.String != c.BaseVersionID {
			return Skill{}, fmt.Errorf("%w: base version changed", ErrRevisionConflict)
		}
		if c.CanonicalName != name || c.ScopeKind != scopeKind || c.ScopeRef != scopeRef || c.Origin != origin || c.Owner != owner {
			return Skill{}, ErrImmutableMetadata
		}
		parentVersion = current.String
	}
	versionID := identity.New("ver")
	_, err = tx.ExecContext(ctx, `INSERT INTO skill_versions(
		id, skill_id, parent_version_id, content_hash, package_blob_ref, manifest_json,
		author_actor, change_message, created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		versionID, skillID, nullIfEmpty(parentVersion), c.CandidateHash, c.CandidateBlobRef,
		string(manifestJSON), actor, c.Reason, formatTime(now))
	if err != nil {
		return Skill{}, fmt.Errorf("insert skill version: %w", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE skills SET state=?, current_version_id=?, enabled=1, updated_at=? WHERE id=?`,
		StateActive, versionID, formatTime(now), skillID)
	if err != nil {
		return Skill{}, fmt.Errorf("activate skill version: %w", err)
	}
	res, err := tx.ExecContext(ctx, `UPDATE skill_candidates SET state=?, reviewed_by=?, review_reason='approved',
		revision=revision+1, updated_at=? WHERE id=? AND revision=?`, CandidatePromoted, actor, formatTime(now), id, c.Revision)
	if err != nil {
		return Skill{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return Skill{}, ErrRevisionConflict
	}
	if err := s.appendEvent(ctx, tx, eventInput{SkillID: skillID, VersionID: versionID, CandidateID: id, Kind: "promoted", ActorKind: actor, Payload: map[string]any{"base_version_id": parentVersion, "content_hash": c.CandidateHash}}); err != nil {
		return Skill{}, err
	}
	if err := tx.Commit(); err != nil {
		return Skill{}, fmt.Errorf("commit promotion: %w", err)
	}
	return s.GetSkill(ctx, skillID)
}

func (s *Service) RejectCandidate(ctx context.Context, id, actor, reason string, expectedRevision int) error {
	if actor == "" || strings.TrimSpace(reason) == "" {
		return fmt.Errorf("actor and rejection reason are required")
	}
	res, err := s.store.DB.ExecContext(ctx, `UPDATE skill_candidates SET state=?, reviewed_by=?, review_reason=?, revision=revision+1,
		updated_at=? WHERE id=? AND state IN (?,?) AND (?=0 OR revision=?)`, CandidateRejected, actor, reason,
		formatTime(time.Now().UTC()), id, CandidateNeedsReview, CandidateQuarantined, expectedRevision, expectedRevision)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrRevisionConflict
	}
	return s.appendEvent(ctx, nil, eventInput{CandidateID: id, Kind: "rejected", ActorKind: actor, Payload: map[string]any{"reason": reason}})
}

func (s *Service) ListSkills(ctx context.Context, includeArchived bool) ([]Skill, error) {
	query := `SELECT s.id, s.canonical_name, s.scope_kind, s.scope_ref, s.origin, s.owner, s.state,
		COALESCE(s.current_version_id,''), s.enabled, s.pinned, s.protected, COALESCE(s.absorbed_into_id,''),
		s.created_at, s.updated_at, COALESCE(v.manifest_json,'{}'), COUNT(a.id),
		COALESCE(SUM(a.body_injected),0), COALESCE(SUM(CASE WHEN a.outcome='success' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN a.outcome='failure' THEN 1 ELSE 0 END),0), MAX(a.created_at)
		FROM skills s LEFT JOIN skill_versions v ON v.id=s.current_version_id
		LEFT JOIN skill_activations a ON a.skill_id=s.id`
	if !includeArchived {
		query += ` WHERE s.state <> 'archived'`
	}
	query += ` GROUP BY s.id ORDER BY s.pinned DESC, s.updated_at DESC, s.canonical_name`
	rows, err := s.store.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Skill, 0)
	for rows.Next() {
		var item Skill
		var created, updated string
		var manifestJSON string
		var last sql.NullString
		if err := rows.Scan(&item.ID, &item.CanonicalName, &item.ScopeKind, &item.ScopeRef, &item.Origin,
			&item.Owner, &item.State, &item.CurrentVersionID, &item.Enabled, &item.Pinned, &item.Protected,
			&item.AbsorbedIntoID, &created, &updated, &manifestJSON, &item.SelectedCount, &item.InjectedCount,
			&item.SuccessCount, &item.FailureCount, &last); err != nil {
			return nil, err
		}
		item.CreatedAt, _ = parseTime(created)
		item.UpdatedAt, _ = parseTime(updated)
		var manifest Manifest
		_ = json.Unmarshal([]byte(manifestJSON), &manifest)
		item.Summary = manifest.Description
		if last.Valid {
			t, _ := parseTime(last.String)
			item.LastUsedAt = &t
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) GetSkill(ctx context.Context, id string) (Skill, error) {
	items, err := s.ListSkills(ctx, true)
	if err != nil {
		return Skill{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return Skill{}, ErrNotFound
}

func (s *Service) UpdateSkillControls(ctx context.Context, id string, input UpdateSkillControlsInput) (Skill, error) {
	input.Actor = strings.TrimSpace(input.Actor)
	if input.Actor == "" || (input.Enabled == nil && input.Pinned == nil) {
		return Skill{}, fmt.Errorf("Skill control update requires actor and at least one field")
	}
	current, err := s.GetSkill(ctx, id)
	if err != nil {
		return Skill{}, err
	}
	if current.State == StateArchived {
		return Skill{}, fmt.Errorf("archived Skills must be restored as candidates")
	}
	if input.ExpectedVersionID == "" || input.ExpectedVersionID != current.CurrentVersionID {
		return Skill{}, ErrRevisionConflict
	}
	enabled, pinned := current.Enabled, current.Pinned
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	if input.Pinned != nil {
		pinned = *input.Pinned
	}
	now := time.Now().UTC()
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return Skill{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE skills SET enabled=?,pinned=?,updated_at=?
		WHERE id=? AND current_version_id=? AND state<>'archived'`, boolInt(enabled), boolInt(pinned), formatTime(now), id, input.ExpectedVersionID)
	if err != nil {
		return Skill{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return Skill{}, ErrRevisionConflict
	}
	if err := s.appendEvent(ctx, tx, eventInput{SkillID: id, VersionID: current.CurrentVersionID,
		Kind: "controls_updated", ActorKind: input.Actor, Payload: map[string]any{"enabled": enabled, "pinned": pinned}}); err != nil {
		return Skill{}, err
	}
	if err := tx.Commit(); err != nil {
		return Skill{}, err
	}
	return s.GetSkill(ctx, id)
}

func (s *Service) GetVersion(ctx context.Context, versionID string) (Version, error) {
	var v Version
	var parent sql.NullString
	var manifestJSON, created string
	err := s.store.DB.QueryRowContext(ctx, `SELECT id, skill_id, parent_version_id, content_hash, package_blob_ref,
		manifest_json, author_actor, change_message, created_at FROM skill_versions WHERE id=?`, versionID).
		Scan(&v.ID, &v.SkillID, &parent, &v.ContentHash, &v.PackageBlobRef, &manifestJSON,
			&v.AuthorActor, &v.ChangeMessage, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Version{}, ErrNotFound
	}
	if err != nil {
		return Version{}, err
	}
	v.ParentVersionID = parent.String
	v.CreatedAt, _ = parseTime(created)
	_ = json.Unmarshal([]byte(manifestJSON), &v.Manifest)
	pkg, err := s.packageByRef(v.PackageBlobRef)
	if err != nil {
		return Version{}, err
	}
	v.Markdown = pkg.Markdown()
	v.SupportingFiles = pkg.SupportingPaths()
	return v, nil
}

func (s *Service) ArchiveSkill(ctx context.Context, skillID, actor, reason, absorbedInto string) (Archive, error) {
	if actor == "" || strings.TrimSpace(reason) == "" {
		return Archive{}, fmt.Errorf("actor and archive reason are required")
	}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return Archive{}, err
	}
	defer tx.Rollback()
	var name, versionID, state string
	var enabled, pinned, protected bool
	err = tx.QueryRowContext(ctx, `SELECT canonical_name, COALESCE(current_version_id,''), state, enabled, pinned, protected FROM skills WHERE id=?`, skillID).
		Scan(&name, &versionID, &state, &enabled, &pinned, &protected)
	if errors.Is(err, sql.ErrNoRows) {
		return Archive{}, ErrNotFound
	}
	if err != nil {
		return Archive{}, err
	}
	if protected {
		return Archive{}, ErrProtectedSkill
	}
	if versionID == "" || state == StateArchived {
		return Archive{}, fmt.Errorf("skill has no active version or is already archived")
	}
	var blobRef string
	if err := tx.QueryRowContext(ctx, `SELECT package_blob_ref FROM skill_versions WHERE id=?`, versionID).Scan(&blobRef); err != nil {
		return Archive{}, err
	}
	if !s.store.Blobs.Exists(blobRef) {
		return Archive{}, fmt.Errorf("archive aborted: version snapshot blob is missing")
	}
	now := time.Now().UTC()
	a := Archive{ID: identity.New("arch"), SkillID: skillID, SkillName: name, ArchivedVersionID: versionID,
		PackageBlobRef: blobRef, PreviousState: state, PreviousEnabled: enabled, PreviousPinned: pinned,
		Reason: reason, ActorKind: actor, AbsorbedIntoID: absorbedInto, CreatedAt: now}
	_, err = tx.ExecContext(ctx, `INSERT INTO skill_archives(id, skill_id, archived_version_id, package_blob_ref,
		previous_state, previous_enabled, previous_pinned, reason, actor_kind, absorbed_into_id, created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, a.ID, skillID, versionID, blobRef, state, enabled, pinned, reason,
		actor, nullIfEmpty(absorbedInto), formatTime(now))
	if err != nil {
		return Archive{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE skills SET state=?, enabled=0, absorbed_into_id=?, updated_at=? WHERE id=?`,
		StateArchived, nullIfEmpty(absorbedInto), formatTime(now), skillID)
	if err != nil {
		return Archive{}, err
	}
	if err := s.appendEvent(ctx, tx, eventInput{SkillID: skillID, VersionID: versionID, Kind: "archived", ActorKind: actor, Payload: map[string]any{"reason": reason, "archive_id": a.ID, "absorbed_into_id": absorbedInto}}); err != nil {
		return Archive{}, err
	}
	if err := tx.Commit(); err != nil {
		return Archive{}, err
	}
	return a, nil
}

func (s *Service) ListArchives(ctx context.Context) ([]Archive, error) {
	rows, err := s.store.DB.QueryContext(ctx, `SELECT a.id, a.skill_id, s.canonical_name, a.archived_version_id,
		a.package_blob_ref, a.previous_state, a.previous_enabled, a.previous_pinned, a.reason, a.actor_kind,
		COALESCE(a.absorbed_into_id,''), a.created_at, COALESCE(a.restored_candidate_id,''), a.restored_at
		FROM skill_archives a JOIN skills s ON s.id=a.skill_id ORDER BY a.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Archive, 0)
	for rows.Next() {
		var a Archive
		var created string
		var restored sql.NullString
		if err := rows.Scan(&a.ID, &a.SkillID, &a.SkillName, &a.ArchivedVersionID, &a.PackageBlobRef,
			&a.PreviousState, &a.PreviousEnabled, &a.PreviousPinned, &a.Reason, &a.ActorKind,
			&a.AbsorbedIntoID, &created, &a.RestoredCandidateID, &restored); err != nil {
			return nil, err
		}
		a.CreatedAt, _ = parseTime(created)
		if restored.Valid {
			t, _ := parseTime(restored.String)
			a.RestoredAt = &t
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Service) RestoreArchive(ctx context.Context, archiveID, actor, reason string) (Candidate, error) {
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" {
		return Candidate{}, fmt.Errorf("actor and restore reason are required")
	}
	var skillID, versionID, blobRef, name, scopeKind, scopeRef, origin, owner string
	var restoredCandidate string
	err := s.store.DB.QueryRowContext(ctx, `SELECT a.skill_id, a.archived_version_id, a.package_blob_ref,
		s.canonical_name, s.scope_kind, s.scope_ref, s.origin, s.owner, COALESCE(a.restored_candidate_id,'') FROM skill_archives a
		JOIN skills s ON s.id=a.skill_id WHERE a.id=?`, archiveID).
		Scan(&skillID, &versionID, &blobRef, &name, &scopeKind, &scopeRef, &origin, &owner, &restoredCandidate)
	if errors.Is(err, sql.ErrNoRows) {
		return Candidate{}, ErrNotFound
	}
	if err != nil {
		return Candidate{}, err
	}
	if restoredCandidate != "" {
		return Candidate{}, fmt.Errorf("%w: archive already has a restore proposal", ErrRevisionConflict)
	}
	pkg, err := s.packageByRef(blobRef)
	if err != nil {
		return Candidate{}, err
	}
	c, err := s.prepareCandidate(CreateCandidateInput{CanonicalName: name, ScopeKind: scopeKind,
		ScopeRef: scopeRef, Origin: origin, Owner: owner, ChangeKind: "restore", TargetSkillID: skillID,
		BaseVersionID: versionID, CreatedBy: actor, TriggerKind: "archive_restore", Reason: reason,
		EvidenceRefs: []string{"archive:" + archiveID}, Markdown: pkg.Markdown(), Files: supportingFiles(pkg)})
	if err != nil {
		return Candidate{}, err
	}
	now := time.Now().UTC()
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return Candidate{}, err
	}
	defer tx.Rollback()
	if err := s.insertCandidate(ctx, tx, c); err != nil {
		return Candidate{}, err
	}
	if err := s.appendEvent(ctx, tx, eventInput{CandidateID: c.ID, Kind: "candidate_created", ActorKind: c.CreatedBy,
		Payload: map[string]any{"state": c.State, "trigger": c.TriggerKind, "archive_id": archiveID}}); err != nil {
		return Candidate{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE skill_archives SET restored_candidate_id=?, restored_at=?
		WHERE id=? AND restored_candidate_id IS NULL`, c.ID, formatTime(now), archiveID)
	if err != nil {
		return Candidate{}, err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return Candidate{}, ErrRevisionConflict
	}
	if err := tx.Commit(); err != nil {
		return Candidate{}, err
	}
	return c, nil
}

func (s *Service) RecordActivation(ctx context.Context, in ActivationInput) (string, error) {
	if in.SessionID == "" || in.TurnID == "" || in.SkillID == "" || in.VersionID == "" {
		return "", fmt.Errorf("session, turn, skill, and version are required")
	}
	if in.Outcome == "" {
		in.Outcome = "unknown"
	}
	if in.AttributionKind == "" {
		in.AttributionKind = "unknown"
	}
	if in.AttributionScore != nil && (*in.AttributionScore < 0 || *in.AttributionScore > 1) {
		return "", fmt.Errorf("attribution score must be between 0 and 1")
	}
	var linked int
	if err := s.store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM skill_versions WHERE id=? AND skill_id=?`, in.VersionID, in.SkillID).Scan(&linked); err != nil {
		return "", err
	}
	if linked != 1 {
		return "", fmt.Errorf("skill version does not belong to the selected skill")
	}
	toolsJSON, _ := json.Marshal(cleanStrings(in.RelevantToolCalls))
	id := identity.New("act")
	now := time.Now().UTC()
	var completed any
	if in.Outcome != "unknown" {
		completed = formatTime(now)
	}
	_, err := s.store.DB.ExecContext(ctx, `INSERT INTO skill_activations(
		id, session_id, turn_id, job_id, skill_id, version_id, selection_source,
		selection_reason, metadata_exposed, body_injected, relevant_tool_calls_json,
		outcome, outcome_source, attribution_kind, attribution_score, created_at, completed_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, in.SessionID, in.TurnID, in.JobID,
		in.SkillID, in.VersionID, in.SelectionSource, in.SelectionReason, in.MetadataExposed,
		in.BodyInjected, string(toolsJSON), in.Outcome, in.OutcomeSource, in.AttributionKind,
		in.AttributionScore, formatTime(now), completed)
	if err != nil {
		return "", err
	}
	return id, nil
}

// CompleteTurnActivations attributes only runtime-observed turn outcomes. It
// never rewrites an already-attributed activation and uses lower confidence
// when several skills were exposed in the same turn.
func (s *Service) CompleteTurnActivations(ctx context.Context, turnID, outcome string) (int64, error) {
	return completeTurnActivations(ctx, s.store.DB, turnID, outcome)
}

// CompleteTurnActivationsTx lets the agent bind activation outcome to the same
// transaction as the terminal turn event and learning outbox record.
func (s *Service) CompleteTurnActivationsTx(ctx context.Context, tx *sql.Tx, turnID, outcome string) (int64, error) {
	if tx == nil {
		return 0, fmt.Errorf("activation outcome requires a transaction")
	}
	return completeTurnActivations(ctx, tx, turnID, outcome)
}

type activationStore interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func completeTurnActivations(ctx context.Context, db activationStore, turnID, outcome string) (int64, error) {
	if outcome != "success" && outcome != "failure" {
		return 0, fmt.Errorf("activation outcome must be success or failure")
	}
	var count, injected int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(body_injected),0)
		FROM skill_activations WHERE turn_id=? AND outcome='unknown'`, turnID).Scan(&count, &injected); err != nil {
		return 0, err
	}
	if count == 0 {
		return 0, nil
	}
	kind, score := "multi_exposed", 0.45
	if injected == 1 && count == 1 {
		kind, score = "single_injected", 0.85
	} else if injected == 0 {
		kind, score = "metadata_only", 0.20
	}
	tools := []string{}
	rows, err := db.QueryContext(ctx, `SELECT metadata_json FROM agent_events
		WHERE turn_id=? AND event_kind='tool_call' ORDER BY sequence`, turnID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var raw string
			if scanErr := rows.Scan(&raw); scanErr != nil {
				return 0, scanErr
			}
			var metadata map[string]any
			if json.Unmarshal([]byte(raw), &metadata) == nil {
				if name, ok := metadata["tool_name"].(string); ok && name != "" {
					tools = append(tools, name)
				}
			}
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			return 0, rowsErr
		}
	}
	toolsJSON, _ := json.Marshal(cleanStrings(tools))
	result, err := db.ExecContext(ctx, `UPDATE skill_activations SET outcome=?,outcome_source='agent_runtime',
		attribution_kind=?,attribution_score=?,relevant_tool_calls_json=?,completed_at=?
		WHERE turn_id=? AND outcome='unknown'`, outcome, kind, score, string(toolsJSON), formatTime(time.Now().UTC()), turnID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Service) packageByRef(ref string) (Package, error) {
	data, err := s.store.Blobs.Get(ref)
	if err != nil {
		return Package{}, err
	}
	return ParsePackage(data)
}

type eventInput struct {
	SkillID, VersionID, CandidateID string
	Kind, ActorKind, ActorRef       string
	Payload                         map[string]any
}

func (s *Service) appendEvent(ctx context.Context, tx *sql.Tx, in eventInput) error {
	payload, err := json.Marshal(in.Payload)
	if err != nil {
		return err
	}
	exec := func(query string, args ...any) (sql.Result, error) {
		return s.store.DB.ExecContext(ctx, query, args...)
	}
	if tx != nil {
		exec = func(query string, args ...any) (sql.Result, error) { return tx.ExecContext(ctx, query, args...) }
	}
	_, err = exec(`INSERT INTO skill_events(id, skill_id, version_id, candidate_id, event_kind,
		actor_kind, actor_ref, payload_json, created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		identity.New("evt"), nullIfEmpty(in.SkillID), nullIfEmpty(in.VersionID), nullIfEmpty(in.CandidateID),
		in.Kind, in.ActorKind, in.ActorRef, string(payload), formatTime(time.Now().UTC()))
	return err
}

const candidateSelect = `SELECT id, canonical_name, scope_kind, scope_ref, origin, owner, change_kind,
	COALESCE(target_skill_id,''), COALESCE(base_version_id,''), candidate_blob_ref, candidate_hash,
	created_by, trigger_kind, reason, evidence_json, state, checks_json, revision,
		COALESCE(reviewed_by,''), COALESCE(review_reason,''), created_at, updated_at,
		COALESCE(source_review_id,'') FROM skill_candidates`

type scanner interface{ Scan(...any) error }

func scanCandidate(row scanner) (Candidate, error) {
	var c Candidate
	var evidenceJSON, checksJSON, created, updated string
	if err := row.Scan(&c.ID, &c.CanonicalName, &c.ScopeKind, &c.ScopeRef, &c.Origin, &c.Owner,
		&c.ChangeKind, &c.TargetSkillID, &c.BaseVersionID, &c.CandidateBlobRef, &c.CandidateHash,
		&c.CreatedBy, &c.TriggerKind, &c.Reason, &evidenceJSON, &c.State, &checksJSON, &c.Revision,
		&c.ReviewedBy, &c.ReviewReason, &created, &updated, &c.SourceReviewID); err != nil {
		return Candidate{}, err
	}
	_ = json.Unmarshal([]byte(evidenceJSON), &c.EvidenceRefs)
	_ = json.Unmarshal([]byte(checksJSON), &c.Checks)
	c.CreatedAt, _ = parseTime(created)
	c.UpdatedAt, _ = parseTime(updated)
	return c, nil
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func formatTime(t time.Time) string         { return t.UTC().Format(time.RFC3339Nano) }
func parseTime(v string) (time.Time, error) { return time.Parse(time.RFC3339Nano, v) }

func cleanStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func supportingFiles(pkg Package) []File {
	var out []File
	for _, file := range pkg.Files {
		if file.Path != "SKILL.md" {
			out = append(out, file)
		}
	}
	return out
}
