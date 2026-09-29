package web

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/secrets"
	"hermetrix-harness/internal/store"
	"hermetrix-harness/internal/taskengine"
)

func TestFacebookReelUploadFinishAndRemoteStatus(t *testing.T) {
	videoBytes := testVideoFixture(t)
	var platformURL string
	var uploaded, finished atomic.Int32
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/me/video_reels" && r.URL.Query().Get("upload_phase") == "start":
			if r.Header.Get("Authorization") != "Bearer page-token" {
				t.Error("start lacks Page token")
			}
			_, _ = io.WriteString(w, `{"video_id":"reel-123","upload_url":"`+platformURL+`/upload/reel-123"}`)
		case r.URL.Path == "/upload/reel-123":
			uploaded.Add(1)
			body, _ := io.ReadAll(r.Body)
			if r.Header.Get("Authorization") != "OAuth page-token" || r.Header.Get("file_size") != strconv.Itoa(len(videoBytes)) || !bytes.Equal(body, videoBytes) {
				t.Error("Reel upload bytes or headers changed")
			}
			_, _ = io.WriteString(w, `{"success":true}`)
		case r.URL.Path == "/me/video_reels" && r.URL.Query().Get("upload_phase") == "finish":
			finished.Add(1)
			if r.URL.Query().Get("video_id") != "reel-123" || r.URL.Query().Get("video_state") != "PUBLISHED" || r.URL.Query().Get("description") != "Reviewed Reel" {
				t.Error("Reel finish changed approved content")
			}
			_, _ = io.WriteString(w, `{"success":true}`)
		case r.URL.Path == "/reel-123" && r.URL.Query().Get("fields") == "status,permalink_url":
			_, _ = io.WriteString(w, `{"id":"reel-123","permalink_url":"https://www.facebook.com/reel/reel-123","status":{"video_status":"ready","publishing_phase":{"status":"complete"}}}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
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
	task, err := tasks.Create(ctx, taskengine.CreateTaskInput{Title: "Reel", Objective: "Publish a reel", OriginalRequest: "Publish", Actor: "owner", Criteria: []taskengine.Criterion{{ID: "AC-1", Description: "Verified Reel"}}})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := dataStore.OwnerPrincipalID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	video, err := content.UploadVideo(ctx, product.VideoUploadInput{Name: "reel.mp4", Reader: bytes.NewReader(videoBytes)})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = dataStore.DB.ExecContext(ctx, `INSERT INTO social_connections(id,owner_principal_id,platform,account_id,display_name,capability,status,checked_at,created_at) VALUES('conn-1',?,'facebook','page-123','Fixture Page','page_identity','ready',?,?)`, owner, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dataStore.DB.ExecContext(ctx, `INSERT INTO task_content_drafts(id,task_id,owner_principal_id,platform,destination,body,script,storyboard,media_artifact_id,media_rights_note,revision,created_at,updated_at) VALUES('draft-1',?,?,'facebook','Fixture Page','Reviewed Reel','','',?,'I own all elements of this video',1,?,?)`, task.ID, owner, video.ID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dataStore.DB.ExecContext(ctx, `INSERT INTO social_posts(id,task_id,draft_id,connection_id,owner_principal_id,subject_hash,intent_hash,route,privacy_level,is_aigc,state,created_at,updated_at) VALUES('post-1',?,'draft-1','conn-1',?,'hash','intent','facebook_reel','',0,'initializing',?,?)`, task.ID, owner, now, now)
	if err != nil {
		t.Fatal(err)
	}
	s := New(nil, nil, nil, nil, nil, nil, nil, nil, dataStore, nil).WithProduct(content).WithSocialVault(vault)
	s.socialFacebookBase = platform.URL
	item := socialPost{ID: "post-1", TaskID: task.ID, DraftID: "draft-1", ConnectionID: "conn-1", Route: "facebook_reel"}
	if err := s.dispatchFacebookReel(ctx, &item, product.ContentDraft{ID: "draft-1", Body: "Reviewed Reel", MediaArtifactID: video.ID}, "page-token"); err != nil {
		t.Fatal(err)
	}
	stored, err := s.socialPostByID(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != "published" || stored.RemoteURL != "https://www.facebook.com/reel/reel-123" || stored.UploadedBytes != int64(len(videoBytes)) || uploaded.Load() != 1 || finished.Load() != 1 {
		t.Fatalf("Reel receipt=%+v uploaded=%d finished=%d", stored, uploaded.Load(), finished.Load())
	}
	if raw, ok := vault.Get(socialUploadRef(item.ID)); !ok || !strings.HasSuffix(raw, "/upload/reel-123") {
		t.Fatal("upload URL was not retained in vault")
	}
	if safeSocialPermalink("javascript:alert(1)", "facebook") != "" {
		t.Fatal("unsafe permalink escaped")
	}
	if parsed, _ := url.Parse(stored.RemoteURL); parsed.Host != "www.facebook.com" {
		t.Fatal("wrong permalink host")
	}
}
