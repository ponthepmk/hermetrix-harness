package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"hermetrix-harness/internal/product"
)

type socialPlatformHTTPError struct{ Status int }

func (e socialPlatformHTTPError) Error() string {
	return fmt.Sprintf("platform returned HTTP %d", e.Status)
}

func (s *Server) dispatchSocialPost(w http.ResponseWriter, r *http.Request) {
	item, err := s.socialPostByID(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if item.State != "approved" || item.ApprovedAt == "" {
		socialPostError(w, errors.New("review and approve this exact export before dispatch"))
		return
	}
	if err := s.socialPostCurrent(r.Context(), item); err != nil {
		socialPostError(w, err)
		return
	}
	connection, err := s.socialConnectionByID(r.Context(), item.ConnectionID)
	if err != nil {
		writeError(w, err)
		return
	}
	token, ok := s.socialVault.Get(socialRef(connection.ID))
	if !ok {
		socialPostError(w, errors.New("account token is missing"))
		return
	}
	var identity socialIdentity
	var status int
	if item.Route == "tiktok_draft" {
		identity, status, err = s.verifyTikTokDraftIdentity(r.Context(), token)
	} else {
		identity, status, err = s.verifySocialIdentity(r.Context(), connection.Platform, token)
	}
	if err != nil {
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	if identity.AccountID != connection.AccountID {
		socialPostError(w, errors.New("account identity changed; reconnect before export"))
		return
	}
	if item.Route == "tiktok_direct" {
		if !item.MusicConsent || item.AllowComment && identity.CommentDisabled || item.AllowDuet && identity.DuetDisabled || item.AllowStitch && identity.StitchDisabled {
			socialPostError(w, errors.New("TikTok posting declaration or interaction settings are no longer valid"))
			return
		}
		allowed := false
		for _, option := range identity.PrivacyOptions {
			if option == item.PrivacyLevel {
				allowed = true
				break
			}
		}
		if !allowed {
			socialPostError(w, errors.New("selected TikTok visibility is unavailable for this account"))
			return
		}
	}
	workspace, err := s.product.GetContentWorkspace(r.Context(), item.TaskID)
	if err != nil {
		writeError(w, err)
		return
	}
	var draft product.ContentDraft
	for _, candidate := range workspace.Drafts {
		if candidate.ID == item.DraftID {
			draft = candidate
			break
		}
	}
	if draft.ID == "" {
		writeError(w, errors.New("approved draft disappeared"))
		return
	}
	if item.Route == "tiktok_direct" {
		if item.BrandContent && item.PrivacyLevel != "PUBLIC_TO_EVERYONE" && item.PrivacyLevel != "MUTUAL_FOLLOW_FRIENDS" {
			socialPostError(w, errors.New("TikTok branded content needs public or friends visibility"))
			return
		}
		duration, parseErr := socialVideoDuration(draft, s.product, r.Context())
		if parseErr != nil || duration <= 0 || identity.MaxVideoPostDurationSec <= 0 || duration > float64(identity.MaxVideoPostDurationSec) {
			socialPostError(w, errors.New("video duration exceeds or cannot be checked against this TikTok creator's current limit"))
			return
		}
	}
	// Durable transition occurs before the first remote effect. A lost response
	// can never turn into a blind replay of the create request.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.store.DB.ExecContext(r.Context(), `UPDATE social_posts SET state='initializing',updated_at=? WHERE id=? AND state='approved'`, now, item.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	if n, _ := result.RowsAffected(); n != 1 {
		socialPostError(w, errors.New("export has already been dispatched"))
		return
	}
	item.State = "initializing"
	switch item.Route {
	case "facebook_feed":
		err = s.dispatchFacebookFeed(r.Context(), &item, draft, token, connection)
	case "facebook_reel":
		err = s.dispatchFacebookReel(r.Context(), &item, draft, token)
	case "tiktok_direct", "tiktok_draft":
		err = s.dispatchTikTokVideo(r.Context(), &item, draft, token)
	default:
		err = errors.New("unsupported export route")
	}
	if err != nil {
		state := "uncertain"
		var platformError socialPlatformHTTPError
		if item.RemoteID == "" && errors.As(err, &platformError) && platformError.Status >= 400 && platformError.Status < 500 {
			state = "failed"
		}
		_ = s.updateSocialPost(context.Background(), item.ID, state, item.RemoteID, item.RemoteStatus, item.RemoteURL, err.Error(), item.UploadedBytes)
		writeJSON(w, http.StatusAccepted, map[string]any{"state": state, "error": err.Error(), "post_id": item.ID})
		return
	}
	updated, err := s.socialPostByID(r.Context(), item.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func socialVideoDuration(draft product.ContentDraft, service *product.Service, ctx context.Context) (float64, error) {
	artifact, err := service.GetArtifactMetadata(ctx, draft.MediaArtifactID)
	if err != nil {
		return 0, err
	}
	switch value := artifact.Metadata["duration_seconds"].(type) {
	case float64:
		return value, nil
	case json.Number:
		return value.Float64()
	default:
		return 0, errors.New("video duration metadata missing")
	}
}

func (s *Server) socialAPI(ctx context.Context, method, endpoint, token, contentType string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, errors.New("could not prepare platform request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	client := s.socialClient
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("platform request did not complete; remote result is uncertain")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, errors.New("platform returned an unreadable response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, socialPlatformHTTPError{Status: resp.StatusCode}
	}
	return data, nil
}

func (s *Server) dispatchFacebookFeed(ctx context.Context, item *socialPost, draft product.ContentDraft, token string, connection socialConnection) error {
	endpoint := strings.TrimRight(s.socialFacebookBase, "/") + "/" + url.PathEscape(connection.AccountID) + "/feed"
	form := url.Values{"message": {draft.Body}}
	data, err := s.socialAPI(ctx, http.MethodPost, endpoint, token, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	var response struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(data, &response) != nil || response.ID == "" {
		return errors.New("Facebook did not return a post id; remote result is uncertain")
	}
	item.RemoteID = response.ID
	item.State = "processing"
	if err := s.updateSocialPost(ctx, item.ID, item.State, item.RemoteID, "created", "", "", 0); err != nil {
		return err
	}
	return s.refreshFacebookPost(ctx, item, token)
}

func (s *Server) dispatchFacebookReel(ctx context.Context, item *socialPost, draft product.ContentDraft, token string) error {
	endpoint := strings.TrimRight(s.socialFacebookBase, "/") + "/me/video_reels?upload_phase=start"
	data, err := s.socialAPI(ctx, http.MethodPost, endpoint, token, "", nil)
	if err != nil {
		return err
	}
	var response struct {
		VideoID   string `json:"video_id"`
		UploadURL string `json:"upload_url"`
	}
	if json.Unmarshal(data, &response) != nil || response.VideoID == "" || !s.allowedSocialUploadURL(response.UploadURL, "facebook") {
		return errors.New("Facebook returned an invalid Reel upload session")
	}
	item.RemoteID = response.VideoID
	item.State = "uploading"
	if err := s.socialVault.Set(socialUploadRef(item.ID), response.UploadURL); err != nil {
		return err
	}
	if err := s.updateSocialPost(ctx, item.ID, item.State, item.RemoteID, "initialized", "", "", 0); err != nil {
		return err
	}
	artifact, file, err := s.product.OpenVideoArtifact(ctx, draft.MediaArtifactID)
	if err != nil {
		return err
	}
	defer file.Close()
	if artifact.ByteSize < 1 {
		return errors.New("video is empty")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, response.UploadURL, file)
	if err != nil {
		return errors.New("could not prepare Reel upload")
	}
	req.ContentLength = int64(artifact.ByteSize)
	req.Header.Set("Authorization", "OAuth "+token)
	req.Header.Set("offset", "0")
	req.Header.Set("file_size", strconv.Itoa(artifact.ByteSize))
	req.Header.Set("Content-Type", "application/octet-stream")
	uploadResponse, err := s.socialUpload(req)
	if err != nil {
		return err
	}
	var uploadReceipt struct {
		Success bool `json:"success"`
	}
	if json.Unmarshal(uploadResponse, &uploadReceipt) != nil || !uploadReceipt.Success {
		return errors.New("Facebook Reel upload was not confirmed; check remote status")
	}
	item.UploadedBytes = int64(artifact.ByteSize)
	item.State = "finishing"
	if err := s.updateSocialPost(ctx, item.ID, item.State, item.RemoteID, "uploaded", "", "", item.UploadedBytes); err != nil {
		return err
	}
	finish := url.Values{"upload_phase": {"finish"}, "video_id": {item.RemoteID}, "video_state": {"PUBLISHED"}, "description": {draft.Body}}
	data, err = s.socialAPI(ctx, http.MethodPost, strings.TrimRight(s.socialFacebookBase, "/")+"/me/video_reels?"+finish.Encode(), token, "", nil)
	if err != nil {
		return err
	}
	var finished struct {
		Success bool `json:"success"`
	}
	if json.Unmarshal(data, &finished) != nil || !finished.Success {
		return errors.New("Facebook did not confirm Reel finish; check its status")
	}
	item.State = "processing"
	if err := s.updateSocialPost(ctx, item.ID, item.State, item.RemoteID, "processing", "", "", item.UploadedBytes); err != nil {
		return err
	}
	return s.refreshFacebookPost(ctx, item, token)
}

func (s *Server) dispatchTikTokVideo(ctx context.Context, item *socialPost, draft product.ContentDraft, token string) error {
	artifact, file, err := s.product.OpenVideoArtifact(ctx, draft.MediaArtifactID)
	if err != nil {
		return err
	}
	defer file.Close()
	if artifact.ByteSize < 1 {
		return errors.New("video is empty")
	}
	const maxChunk int64 = 10 << 20
	size := int64(artifact.ByteSize)
	chunk := size
	if chunk > maxChunk {
		chunk = maxChunk
	}
	count := (size + chunk - 1) / chunk
	request := map[string]any{"source_info": map[string]any{"source": "FILE_UPLOAD", "video_size": size, "chunk_size": chunk, "total_chunk_count": count}}
	initPath := "/v2/post/publish/inbox/video/init/"
	if item.Route == "tiktok_direct" {
		initPath = "/v2/post/publish/video/init/"
		request["post_info"] = map[string]any{"title": draft.Body, "privacy_level": item.PrivacyLevel, "disable_duet": !item.AllowDuet, "disable_stitch": !item.AllowStitch, "disable_comment": !item.AllowComment, "brand_content_toggle": item.BrandContent, "brand_organic_toggle": item.BrandOrganic, "is_aigc": item.IsAIGC}
	}
	encoded, _ := json.Marshal(request)
	data, err := s.socialAPI(ctx, http.MethodPost, strings.TrimRight(s.socialTikTokBase, "/")+initPath, token, "application/json; charset=UTF-8", bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	var response struct {
		Data struct {
			PublishID string `json:"publish_id"`
			UploadURL string `json:"upload_url"`
		} `json:"data"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &response) != nil || response.Error.Code != "ok" || response.Data.PublishID == "" || !s.allowedSocialUploadURL(response.Data.UploadURL, "tiktok") {
		return errors.New("TikTok did not return a valid upload session")
	}
	item.RemoteID = response.Data.PublishID
	item.State = "uploading"
	if err := s.socialVault.Set(socialUploadRef(item.ID), response.Data.UploadURL); err != nil {
		return err
	}
	if err := s.updateSocialPost(ctx, item.ID, item.State, item.RemoteID, "initialized", "", "", 0); err != nil {
		return err
	}
	for offset := int64(0); offset < size; offset += chunk {
		length := chunk
		if size-offset < length {
			length = size - offset
		}
		segment := io.NewSectionReader(file, offset, length)
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, response.Data.UploadURL, segment)
		if err != nil {
			return errors.New("could not prepare TikTok video chunk")
		}
		req.ContentLength = length
		req.Header.Set("Content-Type", artifact.MIMEType)
		req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, offset+length-1, size))
		if _, err := s.socialUpload(req); err != nil {
			return err
		}
		item.UploadedBytes = offset + length
		if err := s.updateSocialPost(ctx, item.ID, "uploading", item.RemoteID, "uploading", "", "", item.UploadedBytes); err != nil {
			return err
		}
	}
	item.State = "processing"
	if err := s.updateSocialPost(ctx, item.ID, item.State, item.RemoteID, "processing", "", "", item.UploadedBytes); err != nil {
		return err
	}
	return s.refreshTikTokPost(ctx, item, token)
}

func socialUploadRef(id string) string { return "social-upload:" + id }

func (s *Server) allowedSocialUploadURL(raw, platform string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Fragment != "" || u.Hostname() == "" {
		return false
	}
	if u.Scheme == "https" && ((platform == "facebook" && u.Hostname() == "rupload.facebook.com") || (platform == "tiktok" && u.Hostname() == "open-upload.tiktokapis.com")) {
		return true
	}
	base := s.socialFacebookBase
	if platform == "tiktok" {
		base = s.socialTikTokBase
	}
	b, err := url.Parse(base)
	if err != nil || u.Scheme != b.Scheme || u.Host != b.Host {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func (s *Server) socialUpload(req *http.Request) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if s.socialClient != nil {
		client.Transport = s.socialClient.Transport
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("video upload did not complete; check remote status before retry")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, errors.New("video upload response was unreadable")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("video upload returned HTTP %d; check remote status", resp.StatusCode)
	}
	return data, nil
}

func (s *Server) refreshSocialPost(w http.ResponseWriter, r *http.Request) {
	item, err := s.socialPostByID(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if item.State == "review" || item.State == "approved" {
		socialPostError(w, errors.New("export has not been dispatched"))
		return
	}
	if item.RemoteID == "" {
		_ = s.updateSocialPost(r.Context(), item.ID, "uncertain", "", "", "", "remote create outcome is unknown; do not dispatch again", item.UploadedBytes)
		writeJSON(w, http.StatusConflict, map[string]any{"error": "remote create outcome is unknown; inspect the platform before any new export"})
		return
	}
	connection, err := s.socialConnectionByID(r.Context(), item.ConnectionID)
	if err != nil {
		writeError(w, err)
		return
	}
	if s.socialVault == nil {
		socialPostError(w, errors.New("account token unavailable"))
		return
	}
	token, ok := s.socialVault.Get(socialRef(connection.ID))
	if !ok {
		socialPostError(w, errors.New("account token unavailable"))
		return
	}
	switch item.Route {
	case "tiktok_direct", "tiktok_draft":
		err = s.refreshTikTokPost(r.Context(), &item, token)
	case "facebook_feed", "facebook_reel":
		err = s.refreshFacebookPost(r.Context(), &item, token)
	default:
		err = errors.New("unsupported export route")
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	updated, err := s.socialPostByID(r.Context(), item.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) refreshTikTokPost(ctx context.Context, item *socialPost, token string) error {
	encoded, _ := json.Marshal(map[string]string{"publish_id": item.RemoteID})
	data, err := s.socialAPI(ctx, http.MethodPost, strings.TrimRight(s.socialTikTokBase, "/")+"/v2/post/publish/status/fetch/", token, "application/json; charset=UTF-8", bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	var response struct {
		Data struct {
			Status        string        `json:"status"`
			FailReason    string        `json:"fail_reason"`
			PublicIDs     []json.Number `json:"publicaly_available_post_id"`
			UploadedBytes int64         `json:"uploaded_bytes"`
		} `json:"data"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &response) != nil || response.Error.Code != "ok" {
		return errors.New("TikTok status check failed")
	}
	state := "processing"
	issue := ""
	remoteURL := ""
	switch response.Data.Status {
	case "PUBLISH_COMPLETE":
		state = "published"
		if len(response.Data.PublicIDs) > 0 {
			remoteURL = "https://www.tiktok.com/@" + url.PathEscape(strings.TrimPrefix(s.itemAccountName(item.ConnectionID), "@")) + "/video/" + response.Data.PublicIDs[0].String()
		}
	case "SEND_TO_USER_INBOX":
		state = "draft_uploaded"
	case "FAILED":
		state = "failed"
		issue = response.Data.FailReason
	case "PROCESSING_UPLOAD", "PROCESSING_DOWNLOAD":
		state = "processing"
	default:
		state = "uncertain"
		issue = "TikTok returned an unknown status"
	}
	if response.Data.UploadedBytes > item.UploadedBytes {
		item.UploadedBytes = response.Data.UploadedBytes
	}
	item.State = state
	item.RemoteStatus = response.Data.Status
	item.RemoteURL = remoteURL
	item.Error = issue
	return s.updateSocialPost(ctx, item.ID, state, item.RemoteID, item.RemoteStatus, remoteURL, issue, item.UploadedBytes)
}

func (s *Server) itemAccountName(id string) string {
	connection, err := s.socialConnectionByID(context.Background(), id)
	if err != nil {
		return ""
	}
	return connection.AccountID
}

func (s *Server) refreshFacebookPost(ctx context.Context, item *socialPost, token string) error {
	fields := "status,permalink_url"
	if item.Route == "facebook_feed" {
		fields = "id,permalink_url,is_published"
	}
	endpoint := strings.TrimRight(s.socialFacebookBase, "/") + "/" + url.PathEscape(item.RemoteID) + "?fields=" + url.QueryEscape(fields)
	data, err := s.socialAPI(ctx, http.MethodGet, endpoint, token, "", nil)
	if err != nil {
		return err
	}
	var response struct {
		ID           string `json:"id"`
		PermalinkURL string `json:"permalink_url"`
		IsPublished  bool   `json:"is_published"`
		Status       struct {
			VideoStatus     string `json:"video_status"`
			PublishingPhase struct {
				Status string `json:"status"`
			} `json:"publishing_phase"`
		} `json:"status"`
	}
	if json.Unmarshal(data, &response) != nil || response.ID == "" {
		return errors.New("Facebook status response did not identify the item")
	}
	state := "processing"
	status := "processing"
	if item.Route == "facebook_feed" {
		if response.IsPublished {
			state = "published"
			status = "published"
		}
	} else {
		status = response.Status.VideoStatus
		if response.Status.PublishingPhase.Status == "complete" {
			state = "published"
		}
	}
	item.State = state
	item.RemoteStatus = status
	item.RemoteURL = safeSocialPermalink(response.PermalinkURL, "facebook")
	return s.updateSocialPost(ctx, item.ID, state, item.RemoteID, status, item.RemoteURL, "", item.UploadedBytes)
}

func safeSocialPermalink(raw, platform string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	u.RawQuery = ""
	if platform == "facebook" && (host == "facebook.com" || host == "www.facebook.com") {
		return u.String()
	}
	if platform == "tiktok" && (host == "tiktok.com" || host == "www.tiktok.com") {
		return u.String()
	}
	return ""
}
