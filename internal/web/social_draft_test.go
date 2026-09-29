package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/secrets"
	"hermetrix-harness/internal/store"
	"hermetrix-harness/internal/taskengine"
)

func TestTikTokDraftRouteKeepsUploadedDraftSeparateFromPublication(t *testing.T) {
	videoBytes := testVideoFixture(t)
	var platformURL string
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/user/info/":
			if r.Header.Get("Authorization") != "Bearer draft-token" {
				t.Error("identity check lacked token")
			}
			_, _ = io.WriteString(w, `{"data":{"user":{"open_id":"open-123","display_name":"Draft Creator"}},"error":{"code":"ok"}}`)
		case "/v2/post/publish/inbox/video/init/":
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request["post_info"] != nil {
				t.Errorf("draft init sent direct-post settings: %+v %v", request, err)
			}
			_, _ = io.WriteString(w, `{"data":{"publish_id":"draft-remote-1","upload_url":"`+platformURL+`/upload"},"error":{"code":"ok"}}`)
		case "/upload":
			body, _ := io.ReadAll(r.Body)
			if !bytes.Equal(body, videoBytes) {
				t.Error("draft upload changed video")
			}
			w.WriteHeader(201)
		case "/v2/post/publish/status/fetch/":
			_, _ = io.WriteString(w, `{"data":{"status":"SEND_TO_USER_INBOX","uploaded_bytes":0},"error":{"code":"ok"}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer platform.Close()
	platformURL = platform.URL
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	vault, err := secrets.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content := product.NewService(dataStore, nil)
	defer content.Close()
	tasks := taskengine.NewService(dataStore)
	task, err := tasks.Create(ctx, taskengine.CreateTaskInput{Title: "Draft upload", Objective: "Send draft", OriginalRequest: "Send TikTok draft", Actor: "owner", Criteria: []taskengine.Criterion{{ID: "AC-1", Description: "Published post"}}})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := dataStore.OwnerPrincipalID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	video, err := content.UploadVideo(ctx, product.VideoUploadInput{Name: "draft.mp4", Reader: bytes.NewReader(videoBytes)})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = dataStore.DB.ExecContext(ctx, `INSERT INTO task_content_briefs(task_id,owner_principal_id,revision,topic,audience,language,facebook_page,tiktok_account,freshness_days,updated_at) VALUES(?,?,1,'Draft','Readers','Thai','','Draft Creator',30,?)`, task.ID, owner, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dataStore.DB.ExecContext(ctx, `INSERT INTO task_content_drafts(id,task_id,owner_principal_id,platform,destination,body,script,storyboard,media_artifact_id,media_rights_note,revision,created_at,updated_at) VALUES('draft-1',?,?,'tiktok','Draft Creator','Reviewed caption','','',?,'I own all visual elements of this clip',1,?,?)`, task.ID, owner, video.ID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	s := New(nil, nil, nil, nil, nil, nil, nil, nil, dataStore, nil).WithProduct(content).WithSocialVault(vault)
	s.socialTikTokBase = platform.URL
	app := httptest.NewServer(s.Handler())
	defer app.Close()
	resp, err := http.Post(app.URL+"/api/social/connections", "application/json", strings.NewReader(`{"platform":"tiktok","mode":"draft","token":"draft-token"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("draft connection=%d %s", resp.StatusCode, body)
	}
	var connection socialConnection
	if err := json.NewDecoder(resp.Body).Decode(&connection); err != nil {
		t.Fatal(err)
	}
	if connection.Capability != "video.upload_identity" || connection.AccountID != "open-123" {
		t.Fatalf("draft identity=%+v", connection)
	}
	_, err = dataStore.DB.ExecContext(ctx, `INSERT INTO social_posts(id,task_id,draft_id,connection_id,owner_principal_id,subject_hash,intent_hash,route,privacy_level,is_aigc,state,created_at,updated_at) VALUES('post-1',?,'draft-1',? ,?,'hash','intent','tiktok_draft','',0,'initializing',?,?)`, task.ID, connection.ID, owner, now, now)
	if err != nil {
		t.Fatal(err)
	}
	item := socialPost{ID: "post-1", TaskID: task.ID, DraftID: "draft-1", ConnectionID: connection.ID, Route: "tiktok_draft"}
	if err := s.dispatchTikTokVideo(ctx, &item, product.ContentDraft{ID: "draft-1", Body: "Reviewed caption", MediaArtifactID: video.ID}, "draft-token"); err != nil {
		t.Fatal(err)
	}
	stored, err := s.socialPostByID(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != "draft_uploaded" || stored.RemoteStatus != "SEND_TO_USER_INBOX" || stored.RemoteURL != "" {
		t.Fatalf("draft was mislabelled as published: %+v", stored)
	}
	// A handoff to the creator's inbox is not a verified published result.
	report, err := tasks.EvaluateCompletion(ctx, task.ID, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	for _, gate := range report.Gates {
		if gate.ID == "content_delivery" && gate.Status == "pass" {
			t.Fatal("inbox draft passed publication gate")
		}
	}
}
