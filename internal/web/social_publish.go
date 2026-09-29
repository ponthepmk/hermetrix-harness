package web

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf16"

	"hermetrix-harness/internal/product"
)

type socialPost struct {
	ID            string `json:"id"`
	TaskID        string `json:"task_id"`
	DraftID       string `json:"draft_id"`
	ConnectionID  string `json:"connection_id"`
	SubjectHash   string `json:"subject_hash"`
	IntentHash    string `json:"intent_hash"`
	Route         string `json:"route"`
	PrivacyLevel  string `json:"privacy_level"`
	IsAIGC        bool   `json:"is_aigc"`
	BrandContent  bool   `json:"brand_content"`
	BrandOrganic  bool   `json:"brand_organic"`
	AllowComment  bool   `json:"allow_comment"`
	AllowDuet     bool   `json:"allow_duet"`
	AllowStitch   bool   `json:"allow_stitch"`
	MusicConsent  bool   `json:"music_consent"`
	State         string `json:"state"`
	RemoteID      string `json:"remote_id,omitempty"`
	RemoteStatus  string `json:"remote_status,omitempty"`
	RemoteURL     string `json:"remote_url,omitempty"`
	UploadedBytes int64  `json:"uploaded_bytes"`
	Error         string `json:"error,omitempty"`
	ApprovedAt    string `json:"approved_at,omitempty"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	RetryOf       string `json:"retry_of,omitempty"`
}

func (s *Server) listSocialPosts(w http.ResponseWriter, r *http.Request) {
	owner, err := s.store.OwnerPrincipalID(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	rows, err := s.store.DB.QueryContext(r.Context(), `SELECT id FROM social_posts WHERE task_id=? AND owner_principal_id=? ORDER BY created_at DESC LIMIT 100`, r.PathValue("id"), owner)
	if err != nil {
		writeError(w, err)
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			writeError(w, err)
			return
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		writeError(w, err)
		return
	}
	items := []socialPost{}
	for _, id := range ids {
		item, err := s.socialPostByID(r.Context(), id)
		if err != nil {
			writeError(w, err)
			return
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) createSocialPost(w http.ResponseWriter, r *http.Request) {
	if s.product == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "content workspace unavailable"})
		return
	}
	var input struct {
		DraftID      string `json:"draft_id"`
		ConnectionID string `json:"connection_id"`
		PrivacyLevel string `json:"privacy_level"`
		IsAIGC       bool   `json:"is_aigc"`
		BrandContent bool   `json:"brand_content"`
		BrandOrganic bool   `json:"brand_organic"`
		AllowComment bool   `json:"allow_comment"`
		AllowDuet    bool   `json:"allow_duet"`
		AllowStitch  bool   `json:"allow_stitch"`
		MusicConsent bool   `json:"music_consent"`
	}
	if !decodeJSONLimit(w, r, &input, 4096) {
		return
	}
	workspace, err := s.product.GetContentWorkspace(r.Context(), r.PathValue("id"))
	if err != nil {
		contentError(w, err)
		return
	}
	var draft *product.ContentDraft
	for i := range workspace.Drafts {
		if workspace.Drafts[i].ID == input.DraftID {
			draft = &workspace.Drafts[i]
			break
		}
	}
	if draft == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "content draft was not found"})
		return
	}
	if !draft.Approved || !draft.ApprovalReady {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "review and approve the current draft before export"})
		return
	}
	connection, err := s.socialConnectionByID(r.Context(), input.ConnectionID)
	if err != nil {
		writeError(w, err)
		return
	}
	if connection.Platform != draft.Platform || !socialDestinationMatches(connection, *draft) || connection.Status != "ready" || s.socialVault == nil || !s.socialVault.Has(socialRef(connection.ID)) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "the approved draft destination does not match a ready connected account"})
		return
	}
	route := "facebook_feed"
	if draft.Platform == "facebook" && draft.MediaArtifactID != "" {
		route = "facebook_reel"
	}
	if draft.Platform == "tiktok" {
		if connection.Capability == "video.upload_identity" {
			route = "tiktok_draft"
		} else {
			route = "tiktok_direct"
		}
	}
	if route == "tiktok_direct" {
		if !input.MusicConsent {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "confirm TikTok Music Usage before preparing Direct Post"})
			return
		}
		allowed := map[string]bool{"SELF_ONLY": true, "MUTUAL_FOLLOW_FRIENDS": true, "FOLLOWER_OF_CREATOR": true, "PUBLIC_TO_EVERYONE": true}
		if !allowed[input.PrivacyLevel] {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "choose a TikTok visibility option"})
			return
		}
		if len(utf16.Encode([]rune(draft.Body))) > 2200 {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "TikTok caption exceeds 2200 characters"})
			return
		}
		if input.BrandContent && input.PrivacyLevel != "PUBLIC_TO_EVERYONE" && input.PrivacyLevel != "MUTUAL_FOLLOW_FRIENDS" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "TikTok branded content needs public or friends visibility"})
			return
		}
	} else if input.PrivacyLevel != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "visibility is selected on the destination platform for this route"})
		return
	}
	if route != "tiktok_direct" && (input.BrandContent || input.BrandOrganic) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "commercial content settings are selected in TikTok for this route"})
		return
	}
	if route != "tiktok_direct" && (input.AllowComment || input.AllowDuet || input.AllowStitch || input.MusicConsent) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "TikTok interaction and music settings require Direct Post"})
		return
	}
	if route == "tiktok_direct" {
		token, ok := s.socialVault.Get(socialRef(connection.ID))
		if !ok {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "TikTok token is missing"})
			return
		}
		creator, status, err := s.verifySocialIdentity(r.Context(), "tiktok", token)
		if err != nil {
			writeJSON(w, status, map[string]any{"error": err.Error()})
			return
		}
		if creator.AccountID != connection.AccountID {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "TikTok account identity changed"})
			return
		}
		allowed := false
		for _, option := range creator.PrivacyOptions {
			allowed = allowed || option == input.PrivacyLevel
		}
		if !allowed {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "selected TikTok visibility is unavailable for this account"})
			return
		}
		if input.AllowComment && creator.CommentDisabled || input.AllowDuet && creator.DuetDisabled || input.AllowStitch && creator.StitchDisabled {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "a selected TikTok interaction is disabled for this account"})
			return
		}
		duration, err := socialVideoDuration(*draft, s.product, r.Context())
		if err != nil || creator.MaxVideoPostDurationSec <= 0 || duration <= 0 || duration > float64(creator.MaxVideoPostDurationSec) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "video duration exceeds or cannot be checked against this TikTok creator's current limit"})
			return
		}
	}
	if draft.MediaArtifactID != "" {
		artifact, err := s.product.GetArtifactMetadata(r.Context(), draft.MediaArtifactID)
		if err != nil {
			writeError(w, err)
			return
		}
		if artifact.Metadata["source"] == "comfy-mcp" && !input.IsAIGC {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "Comfy generated video must be marked as AI generated"})
			return
		}
	}
	intent, _ := json.Marshal(struct {
		SubjectHash, ConnectionID, AccountID, Route, PrivacyLevel                              string
		IsAIGC, BrandContent, BrandOrganic, AllowComment, AllowDuet, AllowStitch, MusicConsent bool
	}{draft.SubjectHash, connection.ID, connection.AccountID, route, input.PrivacyLevel, input.IsAIGC, input.BrandContent, input.BrandOrganic,
		input.AllowComment, input.AllowDuet, input.AllowStitch, input.MusicConsent})
	sum := sha256.Sum256(intent)
	intentHash := hex.EncodeToString(sum[:])
	owner, err := s.store.OwnerPrincipalID(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	id, err := newSocialID()
	if err != nil {
		writeError(w, err)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.store.DB.ExecContext(r.Context(), `INSERT OR IGNORE INTO social_posts(id,task_id,draft_id,connection_id,owner_principal_id,subject_hash,intent_hash,route,privacy_level,is_aigc,brand_content,brand_organic,allow_comment,allow_duet,allow_stitch,music_consent,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, r.PathValue("id"), draft.ID, connection.ID, owner, draft.SubjectHash, intentHash, route, input.PrivacyLevel, input.IsAIGC, input.BrandContent, input.BrandOrganic, input.AllowComment, input.AllowDuet, input.AllowStitch, input.MusicConsent, "review", now, now)
	if err != nil {
		writeError(w, err)
		return
	}
	var currentID string
	if err := s.store.DB.QueryRowContext(r.Context(), `SELECT id FROM social_posts WHERE owner_principal_id=? AND draft_id=? AND connection_id=? AND intent_hash=?`, owner, draft.ID, connection.ID, intentHash).Scan(&currentID); err != nil {
		writeError(w, err)
		return
	}
	item, err := s.socialPostByID(r.Context(), currentID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func socialDestinationMatches(connection socialConnection, draft product.ContentDraft) bool {
	dest := strings.TrimSpace(draft.Destination)
	return dest == connection.AccountID || dest == connection.DisplayName || (connection.Platform == "tiktok" && dest == "@"+connection.AccountID)
}

func (s *Server) approveSocialPost(w http.ResponseWriter, r *http.Request) {
	var input struct {
		IntentHash string `json:"intent_hash"`
	}
	if !decodeJSONLimit(w, r, &input, 1024) {
		return
	}
	item, err := s.socialPostByID(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if item.State != "review" || input.IntentHash != item.IntentHash {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "export preview changed; review it again"})
		return
	}
	if err := s.socialPostCurrent(r.Context(), item); err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.store.DB.ExecContext(r.Context(), `UPDATE social_posts SET state='approved',approved_at=?,updated_at=? WHERE id=? AND state='review'`, now, now, item.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	if n, _ := result.RowsAffected(); n != 1 {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "export approval changed"})
		return
	}
	item.State = "approved"
	item.ApprovedAt = now
	item.UpdatedAt = now
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) socialPostByID(ctx context.Context, id string) (socialPost, error) {
	owner, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return socialPost{}, err
	}
	var item socialPost
	err = s.store.DB.QueryRowContext(ctx, `SELECT id,task_id,draft_id,connection_id,subject_hash,intent_hash,route,privacy_level,is_aigc,brand_content,brand_organic,allow_comment,allow_duet,allow_stitch,music_consent,state,remote_id,remote_status,remote_url,uploaded_bytes,error,approved_at,created_at,updated_at,retry_of FROM social_posts WHERE id=? AND owner_principal_id=?`, id, owner).
		Scan(&item.ID, &item.TaskID, &item.DraftID, &item.ConnectionID, &item.SubjectHash, &item.IntentHash, &item.Route, &item.PrivacyLevel, &item.IsAIGC, &item.BrandContent, &item.BrandOrganic, &item.AllowComment, &item.AllowDuet, &item.AllowStitch, &item.MusicConsent, &item.State, &item.RemoteID, &item.RemoteStatus, &item.RemoteURL, &item.UploadedBytes, &item.Error, &item.ApprovedAt, &item.CreatedAt, &item.UpdatedAt, &item.RetryOf)
	return item, err
}

func (s *Server) socialPostCurrent(ctx context.Context, item socialPost) error {
	if s.product == nil {
		return errors.New("content workspace unavailable")
	}
	workspace, err := s.product.GetContentWorkspace(ctx, item.TaskID)
	if err != nil {
		return err
	}
	for _, draft := range workspace.Drafts {
		if draft.ID == item.DraftID {
			if draft.SubjectHash != item.SubjectHash || !draft.Approved || !draft.ApprovalReady {
				return errors.New("approved content changed; create a new export preview")
			}
			connection, err := s.socialConnectionByID(ctx, item.ConnectionID)
			if err != nil {
				return err
			}
			if connection.Status != "ready" || connection.Platform != draft.Platform || !socialDestinationMatches(connection, draft) || s.socialVault == nil || !s.socialVault.Has(socialRef(connection.ID)) {
				return errors.New("destination account is not ready or no longer matches")
			}
			return nil
		}
	}
	return sql.ErrNoRows
}

func (s *Server) updateSocialPost(ctx context.Context, id, state, remoteID, remoteStatus, remoteURL, errorText string, uploaded int64) error {
	_, err := s.store.DB.ExecContext(ctx, `UPDATE social_posts SET state=?,remote_id=?,remote_status=?,remote_url=?,uploaded_bytes=?,error=?,updated_at=? WHERE id=?`, state, remoteID, remoteStatus, remoteURL, uploaded, errorText, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func socialPostError(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusConflict, map[string]any{"error": fmt.Sprint(err)})
}

func (s *Server) retryFailedSocialPost(w http.ResponseWriter, r *http.Request) {
	previous, err := s.socialPostByID(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if previous.State != "failed" || previous.RemoteID != "" && previous.RemoteStatus != "FAILED" {
		socialPostError(w, errors.New("only a confirmed failed export can be prepared again"))
		return
	}
	if err := s.socialPostCurrent(r.Context(), previous); err != nil {
		socialPostError(w, err)
		return
	}
	owner, err := s.store.OwnerPrincipalID(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	var existingID string
	err = s.store.DB.QueryRowContext(r.Context(), `SELECT id FROM social_posts WHERE retry_of=? AND owner_principal_id=?`, previous.ID, owner).Scan(&existingID)
	if err == nil {
		item, readErr := s.socialPostByID(r.Context(), existingID)
		if readErr != nil {
			writeError(w, readErr)
			return
		}
		writeJSON(w, http.StatusOK, item)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		writeError(w, err)
		return
	}
	id, err := newSocialID()
	if err != nil {
		writeError(w, err)
		return
	}
	hash := sha256.Sum256([]byte(previous.IntentHash + ":retry:" + id))
	intentHash := hex.EncodeToString(hash[:])
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.store.DB.ExecContext(r.Context(), `INSERT INTO social_posts(id,task_id,draft_id,connection_id,owner_principal_id,subject_hash,intent_hash,route,privacy_level,is_aigc,brand_content,brand_organic,allow_comment,allow_duet,allow_stitch,music_consent,state,created_at,updated_at,retry_of) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, previous.TaskID, previous.DraftID, previous.ConnectionID, owner, previous.SubjectHash, intentHash, previous.Route, previous.PrivacyLevel, previous.IsAIGC, previous.BrandContent, previous.BrandOrganic, previous.AllowComment, previous.AllowDuet, previous.AllowStitch, previous.MusicConsent, "review", now, now, previous.ID)
	if err != nil {
		if lookupErr := s.store.DB.QueryRowContext(r.Context(), `SELECT id FROM social_posts WHERE retry_of=? AND owner_principal_id=?`, previous.ID, owner).Scan(&existingID); lookupErr != nil {
			writeError(w, err)
			return
		}
		id = existingID
	}
	item, err := s.socialPostByID(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
