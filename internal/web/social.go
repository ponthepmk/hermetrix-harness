package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hermetrix-harness/internal/durability"
	"io"
	"net/http"
	"strings"
	"time"
)

// socialCredentialVault keeps platform grants outside the application database.
type socialCredentialVault interface {
	Get(string) (string, bool)
	Has(string) bool
	Set(string, string) error
	Delete(string) error
}

type socialConnection struct {
	ID               string `json:"id"`
	Platform         string `json:"platform"`
	AccountID        string `json:"account_id"`
	DisplayName      string `json:"display_name"`
	Capability       string `json:"capability"`
	Status           string `json:"status"`
	CheckedAt        string `json:"checked_at"`
	CreatedAt        string `json:"created_at"`
	CredentialStored bool   `json:"credential_stored"`
}

type socialIdentity struct {
	AccountID               string
	DisplayName             string
	Capability              string
	PrivacyOptions          []string
	MaxVideoPostDurationSec int
	CommentDisabled         bool
	DuetDisabled            bool
	StitchDisabled          bool
}

func socialRef(id string) string { return "social:" + id }

func (s *Server) listSocialConnections(w http.ResponseWriter, r *http.Request) {
	owner, err := s.store.OwnerPrincipalID(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	rows, err := s.store.DB.QueryContext(r.Context(), `SELECT id,platform,account_id,display_name,capability,status,checked_at,created_at FROM social_connections WHERE owner_principal_id=? ORDER BY platform,created_at`, owner)
	if err != nil {
		writeError(w, err)
		return
	}
	defer rows.Close()
	connections := []socialConnection{}
	for rows.Next() {
		var item socialConnection
		if err := rows.Scan(&item.ID, &item.Platform, &item.AccountID, &item.DisplayName, &item.Capability, &item.Status, &item.CheckedAt, &item.CreatedAt); err != nil {
			writeError(w, err)
			return
		}
		item.CredentialStored = s.socialVault != nil && s.socialVault.Has(socialRef(item.ID))
		connections = append(connections, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, connections)
}

func (s *Server) connectSocialAccount(w http.ResponseWriter, r *http.Request) {
	if s.socialVault == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "credential vault is unavailable"})
		return
	}
	var input struct {
		Platform string `json:"platform"`
		Token    string `json:"token"`
		Mode     string `json:"mode"`
	}
	if !decodeJSONLimit(w, r, &input, 20<<10) {
		return
	}
	input.Platform = strings.ToLower(strings.TrimSpace(input.Platform))
	input.Token = strings.TrimSpace(input.Token)
	if (input.Platform != "facebook" && input.Platform != "tiktok") || input.Token == "" || len(input.Token) > 16<<10 || strings.ContainsAny(input.Token, "\r\n") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "choose Facebook or TikTok and provide one valid access token"})
		return
	}
	if input.Platform != "tiktok" && input.Mode != "" || input.Platform == "tiktok" && input.Mode != "" && input.Mode != "direct" && input.Mode != "draft" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "choose a supported TikTok connection mode"})
		return
	}
	var identity socialIdentity
	var status int
	var err error
	if input.Platform == "tiktok" && input.Mode == "draft" {
		identity, status, err = s.verifyTikTokDraftIdentity(r.Context(), input.Token)
	} else {
		identity, status, err = s.verifySocialIdentity(r.Context(), input.Platform, input.Token)
	}
	if err != nil {
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	owner, err := s.store.OwnerPrincipalID(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	var id string
	err = s.store.DB.QueryRowContext(r.Context(), `SELECT id FROM social_connections WHERE owner_principal_id=? AND platform=? AND account_id=?`, owner, input.Platform, identity.AccountID).Scan(&id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeError(w, err)
		return
	}
	if id == "" {
		id, err = newSocialID()
		if err != nil {
			writeError(w, err)
			return
		}
	}
	ref := socialRef(id)
	previous, hadPrevious := s.socialVault.Get(ref)
	if err := s.socialVault.Set(ref, input.Token); err != nil {
		writeError(w, err)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.store.DB.ExecContext(r.Context(), `INSERT INTO social_connections(id,owner_principal_id,platform,account_id,display_name,capability,status,checked_at,created_at)
		VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(owner_principal_id,platform,account_id) DO UPDATE SET display_name=excluded.display_name,capability=excluded.capability,status=excluded.status,checked_at=excluded.checked_at`,
		id, owner, input.Platform, identity.AccountID, identity.DisplayName, identity.Capability, "ready", now, now)
	if err != nil {
		if hadPrevious {
			_ = s.socialVault.Set(ref, previous)
		} else {
			_ = s.socialVault.Delete(ref)
		}
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, socialConnection{ID: id, Platform: input.Platform, AccountID: identity.AccountID, DisplayName: identity.DisplayName, Capability: identity.Capability, Status: "ready", CheckedAt: now, CreatedAt: now, CredentialStored: true})
}

func (s *Server) checkSocialConnection(w http.ResponseWriter, r *http.Request) {
	item, err := s.socialConnectionByID(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if s.socialVault == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "credential vault is unavailable"})
		return
	}
	token, ok := s.socialVault.Get(socialRef(item.ID))
	if !ok {
		s.setSocialStatus(r.Context(), item.ID, "missing_token")
		writeJSON(w, http.StatusConflict, map[string]any{"error": "access token is missing; reconnect this account"})
		return
	}
	var identity socialIdentity
	var status int
	if item.Platform == "tiktok" && item.Capability == "video.upload_identity" {
		identity, status, err = s.verifyTikTokDraftIdentity(r.Context(), token)
	} else {
		identity, status, err = s.verifySocialIdentity(r.Context(), item.Platform, token)
	}
	if err != nil {
		s.setSocialStatus(r.Context(), item.ID, "needs_reconnect")
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	if identity.AccountID != item.AccountID {
		s.setSocialStatus(r.Context(), item.ID, "identity_changed")
		writeJSON(w, http.StatusConflict, map[string]any{"error": "token resolves to a different account; reconnect the intended destination"})
		return
	}
	item.DisplayName = identity.DisplayName
	item.Capability = identity.Capability
	item.Status = "ready"
	item.CheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
	item.CredentialStored = true
	if _, err := s.store.DB.ExecContext(r.Context(), `UPDATE social_connections SET display_name=?,capability=?,status=?,checked_at=? WHERE id=?`, item.DisplayName, item.Capability, item.Status, item.CheckedAt, item.ID); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) getTikTokCreatorInfo(w http.ResponseWriter, r *http.Request) {
	item, err := s.socialConnectionByID(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if item.Platform != "tiktok" || item.Capability != "video.publish_creator_info" || s.socialVault == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "TikTok Direct Post account is required"})
		return
	}
	token, ok := s.socialVault.Get(socialRef(item.ID))
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "TikTok account token is missing"})
		return
	}
	identity, status, err := s.verifySocialIdentity(r.Context(), "tiktok", token)
	if err != nil {
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	if identity.AccountID != item.AccountID {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "TikTok account identity changed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account_id": identity.AccountID, "display_name": identity.DisplayName,
		"privacy_level_options": identity.PrivacyOptions, "max_video_post_duration_sec": identity.MaxVideoPostDurationSec,
		"comment_disabled": identity.CommentDisabled, "duet_disabled": identity.DuetDisabled, "stitch_disabled": identity.StitchDisabled,
		"checked_at": time.Now().UTC().Format(time.RFC3339Nano)})
}

func (s *Server) setSocialStatus(ctx context.Context, id, status string) {
	durability.Exec("record social connection check").Observe(s.store.DB.ExecContext(ctx, `UPDATE social_connections SET status=?,checked_at=? WHERE id=?`, status, time.Now().UTC().Format(time.RFC3339Nano), id))
}

func (s *Server) disconnectSocialAccount(w http.ResponseWriter, r *http.Request) {
	item, err := s.socialConnectionByID(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if s.socialVault == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "credential vault is unavailable"})
		return
	}
	previous, has := s.socialVault.Get(socialRef(item.ID))
	if err := s.socialVault.Delete(socialRef(item.ID)); err != nil {
		writeError(w, err)
		return
	}
	if _, err := s.store.DB.ExecContext(r.Context(), `DELETE FROM social_connections WHERE id=?`, item.ID); err != nil {
		if has {
			_ = s.socialVault.Set(socialRef(item.ID), previous)
		}
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"disconnected": true})
}

func (s *Server) socialConnectionByID(ctx context.Context, id string) (socialConnection, error) {
	owner, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return socialConnection{}, err
	}
	var item socialConnection
	err = s.store.DB.QueryRowContext(ctx, `SELECT id,platform,account_id,display_name,capability,status,checked_at,created_at FROM social_connections WHERE id=? AND owner_principal_id=?`, id, owner).
		Scan(&item.ID, &item.Platform, &item.AccountID, &item.DisplayName, &item.Capability, &item.Status, &item.CheckedAt, &item.CreatedAt)
	return item, err
}

func newSocialID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "soc_" + hex.EncodeToString(raw[:]), nil
}

func (s *Server) verifySocialIdentity(ctx context.Context, platform, token string) (socialIdentity, int, error) {
	var method, url string
	var body io.Reader
	switch platform {
	case "facebook":
		method = http.MethodGet
		url = strings.TrimRight(s.socialFacebookBase, "/") + "/me?fields=id,name,category"
	case "tiktok":
		method = http.MethodPost
		url = strings.TrimRight(s.socialTikTokBase, "/") + "/v2/post/publish/creator_info/query/"
		body = bytes.NewReader([]byte("{}"))
	default:
		return socialIdentity{}, http.StatusBadRequest, errors.New("unsupported platform")
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return socialIdentity{}, http.StatusBadGateway, errors.New("platform check could not start")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if platform == "tiktok" {
		req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	}
	client := s.socialClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return socialIdentity{}, http.StatusBadGateway, errors.New("platform check did not complete")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil || len(data) > 1<<20 {
		return socialIdentity{}, http.StatusBadGateway, errors.New("platform check returned an unreadable response")
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return socialIdentity{}, http.StatusUnprocessableEntity, errors.New("access token is expired or missing required permission")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return socialIdentity{}, http.StatusBadGateway, fmt.Errorf("platform check failed with HTTP %d", resp.StatusCode)
	}
	if platform == "facebook" {
		var result struct {
			ID       string          `json:"id"`
			Name     string          `json:"name"`
			Category string          `json:"category"`
			Error    json.RawMessage `json:"error"`
		}
		if json.Unmarshal(data, &result) != nil || result.ID == "" || result.Name == "" || result.Category == "" || len(result.Error) > 0 {
			return socialIdentity{}, http.StatusUnprocessableEntity, errors.New("token did not resolve to a Facebook Page with publishing identity")
		}
		return socialIdentity{AccountID: result.ID, DisplayName: result.Name, Capability: "page_identity"}, http.StatusOK, nil
	}
	var result struct {
		Data struct {
			CreatorUsername         string   `json:"creator_username"`
			PrivacyLevelOptions     []string `json:"privacy_level_options"`
			MaxVideoPostDurationSec int      `json:"max_video_post_duration_sec"`
			CommentDisabled         bool     `json:"comment_disabled"`
			DuetDisabled            bool     `json:"duet_disabled"`
			StitchDisabled          bool     `json:"stitch_disabled"`
		} `json:"data"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &result) != nil || result.Error.Code != "ok" || result.Data.CreatorUsername == "" {
		return socialIdentity{}, http.StatusUnprocessableEntity, errors.New("TikTok creator check failed; verify video.publish grant and account access")
	}
	return socialIdentity{AccountID: result.Data.CreatorUsername, DisplayName: "@" + result.Data.CreatorUsername, Capability: "video.publish_creator_info", PrivacyOptions: result.Data.PrivacyLevelOptions,
		MaxVideoPostDurationSec: result.Data.MaxVideoPostDurationSec, CommentDisabled: result.Data.CommentDisabled, DuetDisabled: result.Data.DuetDisabled, StitchDisabled: result.Data.StitchDisabled}, http.StatusOK, nil
}

// Draft upload does not expose creator_info/query. This read-only User Info
// check establishes destination identity; video.upload itself is confirmed by
// TikTok only when the user approves and initializes an actual draft upload.
func (s *Server) verifyTikTokDraftIdentity(ctx context.Context, token string) (socialIdentity, int, error) {
	endpoint := strings.TrimRight(s.socialTikTokBase, "/") + "/v2/user/info/?fields=open_id,display_name"
	data, err := s.socialAPI(ctx, http.MethodGet, endpoint, token, "", nil)
	if err != nil {
		return socialIdentity{}, http.StatusUnprocessableEntity, errors.New("TikTok account identity check failed; grant user.info.basic and video.upload")
	}
	var response struct {
		Data struct {
			User struct {
				OpenID      string `json:"open_id"`
				DisplayName string `json:"display_name"`
			} `json:"user"`
		} `json:"data"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &response) != nil || response.Error.Code != "ok" || response.Data.User.OpenID == "" || response.Data.User.DisplayName == "" {
		return socialIdentity{}, http.StatusUnprocessableEntity, errors.New("TikTok user identity was not returned")
	}
	return socialIdentity{AccountID: response.Data.User.OpenID, DisplayName: response.Data.User.DisplayName, Capability: "video.upload_identity"}, http.StatusOK, nil
}
