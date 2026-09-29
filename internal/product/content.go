package product

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"hermetrix-harness/internal/identity"
)

var (
	ErrContentInvalid  = errors.New("content input is invalid")
	ErrContentStale    = errors.New("content changed; reload and review the current version")
	ErrContentNotReady = errors.New("content claims or media are not ready for approval")
)

type ContentBrief struct {
	TaskID        string    `json:"task_id"`
	Revision      int       `json:"revision"`
	Topic         string    `json:"topic"`
	Audience      string    `json:"audience"`
	Language      string    `json:"language"`
	FacebookPage  string    `json:"facebook_page"`
	TikTokAccount string    `json:"tiktok_account"`
	FreshnessDays int       `json:"freshness_days"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ContentClaim struct {
	ID                        string    `json:"id"`
	TaskID                    string    `json:"task_id"`
	SourceID                  string    `json:"source_id"`
	SourceDigest              string    `json:"source_digest"`
	SourceTitle               string    `json:"source_title"`
	SourceURL                 string    `json:"source_url"`
	Statement                 string    `json:"statement"`
	EvidenceExcerpt           string    `json:"evidence_excerpt"`
	Consequential             bool      `json:"consequential"`
	CorroboratingSourceID     string    `json:"corroborating_source_id,omitempty"`
	CorroboratingSourceDigest string    `json:"corroborating_source_digest,omitempty"`
	CorroboratingSourceTitle  string    `json:"corroborating_source_title,omitempty"`
	CorroboratingSourceURL    string    `json:"corroborating_source_url,omitempty"`
	CorroboratingExcerpt      string    `json:"corroborating_excerpt,omitempty"`
	ReviewStatus              string    `json:"review_status"`
	Current                   bool      `json:"current"`
	Reviewer                  string    `json:"reviewer,omitempty"`
	ReviewedAt                time.Time `json:"reviewed_at,omitempty"`
	CreatedAt                 time.Time `json:"created_at"`
}

type ContentDraft struct {
	ID              string    `json:"id"`
	TaskID          string    `json:"task_id"`
	Platform        string    `json:"platform"`
	Destination     string    `json:"destination"`
	Body            string    `json:"body"`
	Script          string    `json:"script"`
	Storyboard      string    `json:"storyboard"`
	MediaArtifactID string    `json:"media_artifact_id,omitempty"`
	MediaRightsNote string    `json:"media_rights_note,omitempty"`
	Revision        int       `json:"revision"`
	SubjectHash     string    `json:"subject_hash"`
	Approved        bool      `json:"approved"`
	ApprovalReady   bool      `json:"approval_ready"`
	ReadinessReason string    `json:"readiness_reason,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type ContentWorkspace struct {
	Brief  *ContentBrief  `json:"brief,omitempty"`
	Claims []ContentClaim `json:"claims"`
	Drafts []ContentDraft `json:"drafts"`
}

type ContentClaimInput struct {
	SourceID              string `json:"source_id"`
	Statement             string `json:"statement"`
	EvidenceExcerpt       string `json:"evidence_excerpt"`
	Consequential         bool   `json:"consequential"`
	CorroboratingSourceID string `json:"corroborating_source_id"`
	CorroboratingExcerpt  string `json:"corroborating_excerpt"`
}

type ContentDraftInput struct {
	ID               string `json:"id,omitempty"`
	ExpectedRevision int    `json:"expected_revision,omitempty"`
	Platform         string `json:"platform"`
	Destination      string `json:"destination"`
	Body             string `json:"body"`
	Script           string `json:"script"`
	Storyboard       string `json:"storyboard"`
	MediaArtifactID  string `json:"media_artifact_id,omitempty"`
	MediaRightsNote  string `json:"media_rights_note,omitempty"`
}

func (s *Service) SaveContentBrief(ctx context.Context, taskID string, input ContentBrief) (ContentBrief, error) {
	ownerID, err := s.researchTaskOwner(ctx, taskID)
	if err != nil {
		return ContentBrief{}, err
	}
	input.Topic, input.Audience, input.Language = strings.TrimSpace(input.Topic), strings.TrimSpace(input.Audience), strings.TrimSpace(input.Language)
	input.FacebookPage, input.TikTokAccount = strings.TrimSpace(input.FacebookPage), strings.TrimSpace(input.TikTokAccount)
	if !validContentText(input.Topic, 2, 240) || !validContentText(input.Audience, 2, 240) || !validContentText(input.Language, 2, 80) ||
		utf8.RuneCountInString(input.FacebookPage) > 160 || utf8.RuneCountInString(input.TikTokAccount) > 160 ||
		(input.FacebookPage == "" && input.TikTokAccount == "") || input.FreshnessDays < 1 || input.FreshnessDays > 365 {
		return ContentBrief{}, ErrContentInvalid
	}
	var current int
	err = s.store.DB.QueryRowContext(ctx, `SELECT revision FROM task_content_briefs WHERE task_id=? AND owner_principal_id=?`, taskID, ownerID).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ContentBrief{}, err
	}
	if current != input.Revision {
		return ContentBrief{}, ErrContentStale
	}
	now := time.Now().UTC()
	result, err := s.store.DB.ExecContext(ctx, `INSERT INTO task_content_briefs(task_id,owner_principal_id,revision,topic,audience,language,facebook_page,tiktok_account,freshness_days,updated_at)
		VALUES(?,?,1,?,?,?,?,?,?,?) ON CONFLICT(task_id) DO UPDATE SET revision=revision+1,topic=excluded.topic,audience=excluded.audience,
		language=excluded.language,facebook_page=excluded.facebook_page,tiktok_account=excluded.tiktok_account,
		freshness_days=excluded.freshness_days,updated_at=excluded.updated_at WHERE task_content_briefs.revision=? AND task_content_briefs.owner_principal_id=?`,
		taskID, ownerID, input.Topic, input.Audience, input.Language, input.FacebookPage, input.TikTokAccount, input.FreshnessDays, formatTime(now), current, ownerID)
	if err != nil {
		return ContentBrief{}, err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return ContentBrief{}, ErrContentStale
	}
	input.TaskID, input.Revision, input.UpdatedAt = taskID, current+1, now
	return input, nil
}

func (s *Service) GetContentWorkspace(ctx context.Context, taskID string) (ContentWorkspace, error) {
	ownerID, err := s.researchTaskOwner(ctx, taskID)
	if err != nil {
		return ContentWorkspace{}, err
	}
	result := ContentWorkspace{Claims: []ContentClaim{}, Drafts: []ContentDraft{}}
	var brief ContentBrief
	var updated string
	err = s.store.DB.QueryRowContext(ctx, `SELECT revision,topic,audience,language,facebook_page,tiktok_account,freshness_days,updated_at
		FROM task_content_briefs WHERE task_id=? AND owner_principal_id=?`, taskID, ownerID).
		Scan(&brief.Revision, &brief.Topic, &brief.Audience, &brief.Language, &brief.FacebookPage, &brief.TikTokAccount, &brief.FreshnessDays, &updated)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ContentWorkspace{}, err
	}
	if err == nil {
		brief.TaskID = taskID
		brief.UpdatedAt, _ = parseTime(updated)
		result.Brief = &brief
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT c.id,c.source_id,c.source_digest,c.statement,c.evidence_excerpt,c.review_status,c.reviewer,c.reviewed_at,c.created_at,
		r.title,r.source_url,r.source_digest,r.selected,c.consequential,c.corroborating_source_id,c.corroborating_source_digest,
		c.corroborating_excerpt,r2.title,r2.source_url,r2.source_digest,r2.selected
		FROM task_content_claims c JOIN task_research_candidates r ON r.id=c.source_id
		LEFT JOIN task_research_candidates r2 ON r2.id=c.corroborating_source_id
		WHERE c.task_id=? AND c.owner_principal_id=? ORDER BY c.created_at,c.id`, taskID, ownerID)
	if err != nil {
		return ContentWorkspace{}, err
	}
	for rows.Next() {
		var claim ContentClaim
		var reviewed, created, currentDigest string
		var selected bool
		var secondTitle, secondURL, secondDigest sql.NullString
		var secondSelected sql.NullBool
		if err := rows.Scan(&claim.ID, &claim.SourceID, &claim.SourceDigest, &claim.Statement, &claim.EvidenceExcerpt, &claim.ReviewStatus,
			&claim.Reviewer, &reviewed, &created, &claim.SourceTitle, &claim.SourceURL, &currentDigest, &selected,
			&claim.Consequential, &claim.CorroboratingSourceID, &claim.CorroboratingSourceDigest, &claim.CorroboratingExcerpt,
			&secondTitle, &secondURL, &secondDigest, &secondSelected); err != nil {
			rows.Close()
			return ContentWorkspace{}, err
		}
		claim.TaskID = taskID
		claim.CreatedAt, _ = parseTime(created)
		if reviewed != "" {
			claim.ReviewedAt, _ = parseTime(reviewed)
		}
		claim.CorroboratingSourceTitle, claim.CorroboratingSourceURL = secondTitle.String, secondURL.String
		claim.Current = selected && currentDigest == claim.SourceDigest && (!claim.Consequential ||
			claim.CorroboratingSourceID != "" && claim.CorroboratingSourceID != claim.SourceID && secondSelected.Valid && secondSelected.Bool && secondDigest.String == claim.CorroboratingSourceDigest)
		result.Claims = append(result.Claims, claim)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ContentWorkspace{}, err
	}
	rows.Close()
	rows, err = s.store.DB.QueryContext(ctx, `SELECT id,platform,destination,body,script,storyboard,media_artifact_id,media_rights_note,revision,created_at,updated_at
		FROM task_content_drafts WHERE task_id=? AND owner_principal_id=? ORDER BY updated_at DESC,id`, taskID, ownerID)
	if err != nil {
		return ContentWorkspace{}, err
	}
	for rows.Next() {
		var draft ContentDraft
		var created, modified string
		if err := rows.Scan(&draft.ID, &draft.Platform, &draft.Destination, &draft.Body, &draft.Script, &draft.Storyboard,
			&draft.MediaArtifactID, &draft.MediaRightsNote, &draft.Revision, &created, &modified); err != nil {
			rows.Close()
			return ContentWorkspace{}, err
		}
		draft.TaskID = taskID
		draft.CreatedAt, _ = parseTime(created)
		draft.UpdatedAt, _ = parseTime(modified)
		result.Drafts = append(result.Drafts, draft)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ContentWorkspace{}, err
	}
	rows.Close()
	for i := range result.Drafts {
		draft := &result.Drafts[i]
		draft.SubjectHash, err = s.contentSubjectHash(ctx, *draft, result)
		if err != nil {
			return ContentWorkspace{}, err
		}
		var exists int
		err = s.store.DB.QueryRowContext(ctx, `SELECT 1 FROM task_content_approvals WHERE draft_id=? AND task_id=? AND owner_principal_id=? AND subject_hash=?`,
			draft.ID, taskID, ownerID, draft.SubjectHash).Scan(&exists)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return ContentWorkspace{}, err
		}
		draft.Approved = err == nil
		draft.ReadinessReason = contentReadiness(*draft, result)
		draft.ApprovalReady = draft.ReadinessReason == ""
	}
	return result, nil
}

func (s *Service) CreateContentClaim(ctx context.Context, taskID string, input ContentClaimInput) (ContentClaim, error) {
	ownerID, err := s.researchTaskOwner(ctx, taskID)
	if err != nil {
		return ContentClaim{}, err
	}
	input.Statement, input.EvidenceExcerpt = strings.TrimSpace(input.Statement), strings.TrimSpace(input.EvidenceExcerpt)
	input.CorroboratingSourceID, input.CorroboratingExcerpt = strings.TrimSpace(input.CorroboratingSourceID), strings.TrimSpace(input.CorroboratingExcerpt)
	if !validContentText(input.Statement, 5, 1000) || !validContentText(input.EvidenceExcerpt, 10, 3000) {
		return ContentClaim{}, ErrContentInvalid
	}
	if input.Consequential && (input.CorroboratingSourceID == "" || input.CorroboratingSourceID == input.SourceID || !validContentText(input.CorroboratingExcerpt, 10, 3000)) ||
		!input.Consequential && (input.CorroboratingSourceID != "" || input.CorroboratingExcerpt != "") {
		return ContentClaim{}, ErrContentInvalid
	}
	var digest string
	err = s.store.DB.QueryRowContext(ctx, `SELECT source_digest FROM task_research_candidates WHERE id=? AND task_id=? AND owner_principal_id=? AND selected=1`,
		input.SourceID, taskID, ownerID).Scan(&digest)
	if errors.Is(err, sql.ErrNoRows) {
		return ContentClaim{}, ErrContentNotReady
	}
	if err != nil {
		return ContentClaim{}, err
	}
	secondDigest := ""
	if input.Consequential {
		err = s.store.DB.QueryRowContext(ctx, `SELECT source_digest FROM task_research_candidates WHERE id=? AND task_id=? AND owner_principal_id=? AND selected=1`,
			input.CorroboratingSourceID, taskID, ownerID).Scan(&secondDigest)
		if errors.Is(err, sql.ErrNoRows) {
			return ContentClaim{}, ErrContentNotReady
		}
		if err != nil {
			return ContentClaim{}, err
		}
	}
	now := time.Now().UTC()
	item := ContentClaim{ID: identity.New("claim"), TaskID: taskID, SourceID: input.SourceID, SourceDigest: digest,
		Statement: input.Statement, EvidenceExcerpt: input.EvidenceExcerpt, Consequential: input.Consequential, CorroboratingSourceID: input.CorroboratingSourceID,
		CorroboratingSourceDigest: secondDigest, CorroboratingExcerpt: input.CorroboratingExcerpt, ReviewStatus: "pending", Current: true, CreatedAt: now}
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO task_content_claims(id,task_id,owner_principal_id,source_id,source_digest,statement,evidence_excerpt,
		consequential,corroborating_source_id,corroborating_source_digest,corroborating_excerpt,review_status,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,'pending',?)`, item.ID, taskID, ownerID, item.SourceID, item.SourceDigest, item.Statement, item.EvidenceExcerpt,
		item.Consequential, item.CorroboratingSourceID, item.CorroboratingSourceDigest, item.CorroboratingExcerpt, formatTime(now))
	return item, err
}

func (s *Service) ReviewContentClaim(ctx context.Context, taskID, claimID, verdict, actor string) (ContentClaim, error) {
	ownerID, err := s.researchTaskOwner(ctx, taskID)
	if err != nil {
		return ContentClaim{}, err
	}
	if verdict != "supported" && verdict != "unsupported" && verdict != "conflict" || !validContentText(strings.TrimSpace(actor), 1, 160) {
		return ContentClaim{}, ErrContentInvalid
	}
	workspace, err := s.GetContentWorkspace(ctx, taskID)
	if err != nil {
		return ContentClaim{}, err
	}
	var claim *ContentClaim
	for i := range workspace.Claims {
		if workspace.Claims[i].ID == claimID {
			claim = &workspace.Claims[i]
			break
		}
	}
	if claim == nil {
		return ContentClaim{}, ErrResearchNotFound
	}
	if !claim.Current {
		return ContentClaim{}, ErrContentStale
	}
	now := time.Now().UTC()
	result, err := s.store.DB.ExecContext(ctx, `UPDATE task_content_claims SET review_status=?,reviewer=?,reviewed_at=? WHERE id=? AND task_id=? AND owner_principal_id=? AND source_digest=? AND EXISTS
		(SELECT 1 FROM task_research_candidates WHERE id=source_id AND selected=1 AND source_digest=?)
		AND (consequential=0 OR EXISTS (SELECT 1 FROM task_research_candidates WHERE id=corroborating_source_id
		AND selected=1 AND source_digest=corroborating_source_digest))`, verdict, strings.TrimSpace(actor), formatTime(now), claimID, taskID, ownerID, claim.SourceDigest, claim.SourceDigest)
	if err != nil {
		return ContentClaim{}, err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return ContentClaim{}, ErrContentStale
	}
	claim.ReviewStatus, claim.Reviewer, claim.ReviewedAt = verdict, strings.TrimSpace(actor), now
	return *claim, nil
}

func (s *Service) RemoveContentClaim(ctx context.Context, taskID, claimID string) error {
	ownerID, err := s.researchTaskOwner(ctx, taskID)
	if err != nil {
		return err
	}
	result, err := s.store.DB.ExecContext(ctx, `DELETE FROM task_content_claims WHERE id=? AND task_id=? AND owner_principal_id=?`, claimID, taskID, ownerID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return ErrResearchNotFound
	}
	return nil
}

func (s *Service) SaveContentDraft(ctx context.Context, taskID string, input ContentDraftInput) (ContentDraft, error) {
	ownerID, err := s.researchTaskOwner(ctx, taskID)
	if err != nil {
		return ContentDraft{}, err
	}
	input.Destination, input.Body = strings.TrimSpace(input.Destination), strings.TrimSpace(input.Body)
	input.Script, input.Storyboard = strings.TrimSpace(input.Script), strings.TrimSpace(input.Storyboard)
	input.MediaRightsNote = strings.TrimSpace(input.MediaRightsNote)
	if input.Platform != "facebook" && input.Platform != "tiktok" || !validContentText(input.Destination, 1, 160) ||
		!validContentText(input.Body, 1, 5000) || utf8.RuneCountInString(input.Script) > 10000 || utf8.RuneCountInString(input.Storyboard) > 10000 || utf8.RuneCountInString(input.MediaRightsNote) > 2000 {
		return ContentDraft{}, ErrContentInvalid
	}
	workspace, err := s.GetContentWorkspace(ctx, taskID)
	if err != nil {
		return ContentDraft{}, err
	}
	if workspace.Brief == nil {
		return ContentDraft{}, ErrContentNotReady
	}
	if input.Platform == "facebook" && workspace.Brief.FacebookPage != input.Destination || input.Platform == "tiktok" && workspace.Brief.TikTokAccount != input.Destination {
		return ContentDraft{}, ErrContentInvalid
	}
	if input.MediaArtifactID != "" {
		var kind, projectID, mimeType, metadataJSON string
		var taskProject sql.NullString
		if err := s.store.DB.QueryRowContext(ctx, `SELECT project_id FROM durable_tasks WHERE id=? AND owner_principal_id=?`, taskID, ownerID).Scan(&taskProject); err != nil {
			return ContentDraft{}, err
		}
		err = s.store.DB.QueryRowContext(ctx, `SELECT kind,mime_type,metadata_json,COALESCE(project_id,'') FROM artifacts WHERE id=? AND owner_principal_id=?`, input.MediaArtifactID, ownerID).Scan(&kind, &mimeType, &metadataJSON, &projectID)
		var metadata map[string]any
		if err == nil {
			err = json.Unmarshal([]byte(metadataJSON), &metadata)
		}
		if err != nil || kind != "video" || (mimeType != "video/mp4" && mimeType != "video/webm") || metadata["validation"] != "ffprobe-v1" || projectID != taskProject.String {
			return ContentDraft{}, ErrContentInvalid
		}
		_, file, openErr := s.OpenVideoArtifact(ctx, input.MediaArtifactID)
		if openErr != nil {
			return ContentDraft{}, ErrContentInvalid
		}
		detected, _, probeErr := probeVideo(ctx, file)
		file.Close()
		if probeErr != nil || detected != mimeType {
			return ContentDraft{}, ErrContentInvalid
		}
	}
	now := time.Now().UTC()
	item := ContentDraft{ID: input.ID, TaskID: taskID, Platform: input.Platform, Destination: input.Destination, Body: input.Body,
		Script: input.Script, Storyboard: input.Storyboard, MediaArtifactID: input.MediaArtifactID, MediaRightsNote: input.MediaRightsNote, UpdatedAt: now}
	if input.ID == "" {
		if input.ExpectedRevision != 0 {
			return ContentDraft{}, ErrContentStale
		}
		item.ID, item.Revision, item.CreatedAt = identity.New("draft"), 1, now
		_, err = s.store.DB.ExecContext(ctx, `INSERT INTO task_content_drafts(id,task_id,owner_principal_id,platform,destination,body,script,storyboard,media_artifact_id,media_rights_note,revision,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,1,?,?)`, item.ID, taskID, ownerID, item.Platform, item.Destination, item.Body, item.Script, item.Storyboard, item.MediaArtifactID, item.MediaRightsNote, formatTime(now), formatTime(now))
	} else {
		result, updateErr := s.store.DB.ExecContext(ctx, `UPDATE task_content_drafts SET platform=?,destination=?,body=?,script=?,storyboard=?,media_artifact_id=?,media_rights_note=?,revision=revision+1,updated_at=?
			WHERE id=? AND task_id=? AND owner_principal_id=? AND revision=?`, item.Platform, item.Destination, item.Body, item.Script, item.Storyboard, item.MediaArtifactID, item.MediaRightsNote, formatTime(now), input.ID, taskID, ownerID, input.ExpectedRevision)
		if updateErr != nil {
			return ContentDraft{}, updateErr
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return ContentDraft{}, ErrContentStale
		}
		item.Revision = input.ExpectedRevision + 1
	}
	if err != nil {
		return ContentDraft{}, err
	}
	workspace, err = s.GetContentWorkspace(ctx, taskID)
	if err != nil {
		return ContentDraft{}, err
	}
	for _, draft := range workspace.Drafts {
		if draft.ID == item.ID {
			return draft, nil
		}
	}
	return ContentDraft{}, ErrResearchNotFound
}

func (s *Service) ApproveContentDraft(ctx context.Context, taskID, draftID, subjectHash, actor string) (ContentDraft, error) {
	ownerID, err := s.researchTaskOwner(ctx, taskID)
	if err != nil {
		return ContentDraft{}, err
	}
	workspace, err := s.GetContentWorkspace(ctx, taskID)
	if err != nil {
		return ContentDraft{}, err
	}
	for _, draft := range workspace.Drafts {
		if draft.ID != draftID {
			continue
		}
		if draft.SubjectHash != subjectHash {
			return ContentDraft{}, ErrContentStale
		}
		if !draft.ApprovalReady {
			return ContentDraft{}, fmt.Errorf("%w: %s", ErrContentNotReady, draft.ReadinessReason)
		}
		if !validContentText(strings.TrimSpace(actor), 1, 160) {
			return ContentDraft{}, ErrContentInvalid
		}
		_, err = s.store.DB.ExecContext(ctx, `INSERT OR IGNORE INTO task_content_approvals(id,draft_id,task_id,owner_principal_id,subject_hash,actor,approved_at)
			VALUES(?,?,?,?,?,?,?)`, identity.New("contentapproval"), draftID, taskID, ownerID, subjectHash, strings.TrimSpace(actor), formatTime(time.Now().UTC()))
		if err != nil {
			return ContentDraft{}, err
		}
		// The publisher must re-evaluate this subject hash and all readiness
		// conditions immediately before any future remote dispatch.
		draft.Approved = true
		return draft, nil
	}
	return ContentDraft{}, ErrResearchNotFound
}

func (s *Service) contentSubjectHash(ctx context.Context, draft ContentDraft, workspace ContentWorkspace) (string, error) {
	var taskRevision int
	if err := s.store.DB.QueryRowContext(ctx, `SELECT revision FROM durable_tasks WHERE id=?`, draft.TaskID).Scan(&taskRevision); err != nil {
		return "", err
	}
	mediaChecksum := ""
	if draft.MediaArtifactID != "" {
		if err := s.store.DB.QueryRowContext(ctx, `SELECT checksum FROM artifacts WHERE id=?`, draft.MediaArtifactID).Scan(&mediaChecksum); err != nil {
			return "", err
		}
	}
	briefRevision := 0
	if workspace.Brief != nil {
		briefRevision = workspace.Brief.Revision
	}
	claimBindings := make([]string, 0, len(workspace.Claims))
	for _, claim := range workspace.Claims {
		claimBindings = append(claimBindings, claim.ID+":"+claim.SourceDigest+":"+claim.ReviewStatus+":"+fmt.Sprint(claim.Current)+":"+
			fmt.Sprint(claim.Consequential)+":"+claim.CorroboratingSourceID+":"+claim.CorroboratingSourceDigest+":"+claim.CorroboratingExcerpt)
	}
	payload, _ := json.Marshal(struct {
		TaskRevision    int
		BriefRevision   int
		DraftID         string
		DraftRevision   int
		Platform        string
		Destination     string
		Body            string
		Script          string
		Storyboard      string
		MediaArtifactID string
		MediaRightsNote string
		MediaChecksum   string
		Claims          []string
	}{taskRevision, briefRevision, draft.ID, draft.Revision, draft.Platform, draft.Destination, draft.Body, draft.Script, draft.Storyboard, draft.MediaArtifactID, draft.MediaRightsNote, mediaChecksum, claimBindings})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func contentReadiness(draft ContentDraft, workspace ContentWorkspace) string {
	if workspace.Brief == nil {
		return "บันทึก brief ก่อน"
	}
	if len(workspace.Claims) == 0 {
		return "เพิ่มและตรวจข้อกล่าวอ้างพร้อมแหล่งข้อมูลก่อน"
	}
	for _, claim := range workspace.Claims {
		if !claim.Current {
			return "แหล่งข้อมูลของข้อกล่าวอ้างเปลี่ยนหรือไม่ได้เลือกแล้ว"
		}
		if claim.ReviewStatus != "supported" {
			return "ยังมีข้อกล่าวอ้างที่ไม่ผ่านการตรวจ"
		}
	}
	if draft.Platform == "tiktok" && draft.MediaArtifactID == "" {
		return "แนบวิดีโอก่อนอนุมัติร่าง TikTok"
	}
	if draft.MediaArtifactID != "" && !validContentText(draft.MediaRightsNote, 10, 2000) {
		return "ระบุสิทธิ์หรือที่มาของวิดีโอก่อนอนุมัติ"
	}
	return ""
}

func validContentText(value string, min, max int) bool {
	n := utf8.RuneCountInString(value)
	return n >= min && n <= max && !strings.ContainsAny(value, "\x00")
}
