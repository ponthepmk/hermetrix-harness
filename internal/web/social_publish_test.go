package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
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

func TestTikTokDirectVideoUploadUsesExactApprovedVisibility(t *testing.T) {
	videoBytes := testVideoFixture(t)
	var platformURL string
	var initCalls, uploadCalls atomic.Int32
	var creatorLimit atomic.Int32
	creatorLimit.Store(600)
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/post/publish/creator_info/query/":
			if r.Header.Get("Authorization") != "Bearer tt-token" {
				w.WriteHeader(401)
				return
			}
			_, _ = io.WriteString(w, `{"data":{"creator_username":"fixture","privacy_level_options":["SELF_ONLY","PUBLIC_TO_EVERYONE"],"max_video_post_duration_sec":`+strconv.Itoa(int(creatorLimit.Load()))+`},"error":{"code":"ok"}}`)
		case "/v2/post/publish/video/init/":
			initCalls.Add(1)
			var body struct {
				PostInfo struct {
					Privacy        string `json:"privacy_level"`
					Title          string `json:"title"`
					IsAIGC         bool   `json:"is_aigc"`
					BrandContent   bool   `json:"brand_content_toggle"`
					BrandOrganic   bool   `json:"brand_organic_toggle"`
					DisableComment bool   `json:"disable_comment"`
					DisableDuet    bool   `json:"disable_duet"`
					DisableStitch  bool   `json:"disable_stitch"`
				} `json:"post_info"`
				SourceInfo struct {
					VideoSize  int `json:"video_size"`
					ChunkSize  int `json:"chunk_size"`
					ChunkCount int `json:"total_chunk_count"`
				} `json:"source_info"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.PostInfo.Privacy != "PUBLIC_TO_EVERYONE" || body.PostInfo.Title != "Video finding" || !body.PostInfo.BrandContent || !body.PostInfo.BrandOrganic || !body.PostInfo.DisableComment || !body.PostInfo.DisableDuet || !body.PostInfo.DisableStitch || body.SourceInfo.VideoSize != len(videoBytes) || body.SourceInfo.ChunkCount != 1 {
				t.Errorf("wrong TikTok init: %+v", body)
			}
			_, _ = io.WriteString(w, `{"data":{"publish_id":"pub-123","upload_url":"`+platformURL+`/upload/video"},"error":{"code":"ok"}}`)
		case "/upload/video":
			uploadCalls.Add(1)
			body, _ := io.ReadAll(r.Body)
			if r.Method != "PUT" || !bytes.Equal(body, videoBytes) || r.Header.Get("Content-Range") != "bytes 0-"+strconv.Itoa(len(videoBytes)-1)+"/"+strconv.Itoa(len(videoBytes)) {
				t.Errorf("wrong TikTok upload")
			}
			w.WriteHeader(201)
		case "/v2/post/publish/status/fetch/":
			_, _ = io.WriteString(w, `{"data":{"status":"PUBLISH_COMPLETE","publicaly_available_post_id":[],"uploaded_bytes":`+strconv.Itoa(len(videoBytes))+`},"error":{"code":"ok"}}`)
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
	task, err := tasks.Create(ctx, taskengine.CreateTaskInput{Title: "Video campaign", Objective: "Publish video", OriginalRequest: "TikTok post", Actor: "owner", Criteria: []taskengine.Criterion{{ID: "AC-1", Description: "Verified publication"}}})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := dataStore.OwnerPrincipalID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dataStore.DB.ExecContext(ctx, `INSERT INTO task_research_candidates(id,task_id,owner_principal_id,query,source_kind,doi,title,publisher,source_url,published_date,published_precision,retrieved_at,source_digest,selected) VALUES('source-video',?,?,'topic','crossref','10.1234/video','Study','Publisher','https://doi.org/10.1234/video','2026-09-25','day',?,'digest-video',1)`, task.ID, owner, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := content.SaveContentBrief(ctx, task.ID, product.ContentBrief{Topic: "Research video", Audience: "Readers", Language: "Thai", TikTokAccount: "@fixture", FreshnessDays: 30}); err != nil {
		t.Fatal(err)
	}
	claim, err := content.CreateContentClaim(ctx, task.ID, product.ContentClaimInput{SourceID: "source-video", Statement: "A supported research finding", EvidenceExcerpt: "The study reports an evidence-based finding."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := content.ReviewContentClaim(ctx, task.ID, claim.ID, "supported", "owner"); err != nil {
		t.Fatal(err)
	}
	video, err := content.UploadVideo(ctx, product.VideoUploadInput{Name: "test.mp4", Reader: bytes.NewReader(videoBytes)})
	if err != nil {
		t.Fatal(err)
	}
	draft, err := content.SaveContentDraft(ctx, task.ID, product.ContentDraftInput{Platform: "tiktok", Destination: "@fixture", Body: "Video finding", MediaArtifactID: video.ID, MediaRightsNote: "I created this clip and own all visual elements"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := content.ApproveContentDraft(ctx, task.ID, draft.ID, draft.SubjectHash, "owner"); err != nil {
		t.Fatal(err)
	}
	s := New(nil, nil, nil, nil, nil, nil, nil, nil, dataStore, nil).WithProduct(content).WithSocialVault(vault)
	s.socialTikTokBase = platform.URL
	app := httptest.NewServer(s.Handler())
	defer app.Close()
	request := func(path, body string, want int) []byte {
		t.Helper()
		req, _ := http.NewRequest("POST", app.URL+path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		payload, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("%s status=%d want=%d body=%s", path, resp.StatusCode, want, payload)
		}
		return payload
	}
	var connection socialConnection
	if err := json.Unmarshal(request("/api/social/connections", `{"platform":"tiktok","token":"tt-token"}`, 200), &connection); err != nil {
		t.Fatal(err)
	}
	creatorResponse, err := http.Get(app.URL + "/api/social/connections/" + connection.ID + "/creator-info")
	if err != nil {
		t.Fatal(err)
	}
	var creatorInfo struct {
		AccountID      string   `json:"account_id"`
		PrivacyOptions []string `json:"privacy_level_options"`
		MaxDuration    int      `json:"max_video_post_duration_sec"`
	}
	if creatorResponse.StatusCode != 200 || json.NewDecoder(creatorResponse.Body).Decode(&creatorInfo) != nil {
		creatorResponse.Body.Close()
		t.Fatalf("creator-info status=%d", creatorResponse.StatusCode)
	}
	creatorResponse.Body.Close()
	if creatorInfo.AccountID != "fixture" || creatorInfo.MaxDuration != 600 || len(creatorInfo.PrivacyOptions) != 2 {
		t.Fatalf("creator-info=%+v", creatorInfo)
	}
	var intent socialPost
	request("/api/tasks/"+task.ID+"/social/posts", `{"draft_id":"`+draft.ID+`","connection_id":"`+connection.ID+`","privacy_level":"SELF_ONLY","brand_content":true,"music_consent":true}`, 422)
	request("/api/tasks/"+task.ID+"/social/posts", `{"draft_id":"`+draft.ID+`","connection_id":"`+connection.ID+`","privacy_level":"PUBLIC_TO_EVERYONE"}`, 422)
	request("/api/tasks/"+task.ID+"/social/posts", `{"draft_id":"`+draft.ID+`","connection_id":"`+connection.ID+`","privacy_level":"FOLLOWER_OF_CREATOR","music_consent":true}`, 422)
	creatorLimit.Store(0)
	request("/api/tasks/"+task.ID+"/social/posts", `{"draft_id":"`+draft.ID+`","connection_id":"`+connection.ID+`","privacy_level":"PUBLIC_TO_EVERYONE","music_consent":true}`, 422)
	creatorLimit.Store(600)
	if err := json.Unmarshal(request("/api/tasks/"+task.ID+"/social/posts", `{"draft_id":"`+draft.ID+`","connection_id":"`+connection.ID+`","privacy_level":"PUBLIC_TO_EVERYONE","brand_content":true,"brand_organic":true,"music_consent":true}`, 200), &intent); err != nil {
		t.Fatal(err)
	}
	if !intent.BrandContent || !intent.BrandOrganic {
		t.Fatalf("commercial settings not retained in reviewed intent: %+v", intent)
	}
	request("/api/social/posts/"+intent.ID+"/approve", `{"intent_hash":"`+intent.IntentHash+`"}`, 200)
	var posted socialPost
	if err := json.Unmarshal(request("/api/social/posts/"+intent.ID+"/dispatch", "", 200), &posted); err != nil {
		t.Fatal(err)
	}
	if posted.State != "published" || posted.RemoteID != "pub-123" || posted.RemoteURL != "" || posted.UploadedBytes != int64(len(videoBytes)) {
		t.Fatalf("TikTok receipt=%+v", posted)
	}
	request("/api/social/posts/"+intent.ID+"/dispatch", "", 409)
	if initCalls.Load() != 1 || uploadCalls.Load() != 1 {
		t.Fatalf("duplicate remote effect: init=%d upload=%d", initCalls.Load(), uploadCalls.Load())
	}
}

func TestFacebookExportNeedsExactApprovalAndVerifiedRemoteReceipt(t *testing.T) {
	const token = "page-token"
	var creates atomic.Int32
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(401)
			return
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/me":
			_, _ = io.WriteString(w, `{"id":"page-123","name":"Fixture Page","category":"Community"}`)
		case r.Method == "POST" && r.URL.Path == "/page-123/feed":
			attempt := creates.Add(1)
			body, _ := io.ReadAll(r.Body)
			form, _ := url.ParseQuery(string(body))
			if form.Get("message") != "Evidence-based post" {
				t.Errorf("published unexpected copy %q", form.Get("message"))
			}
			if attempt == 1 {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = io.WriteString(w, `{"id":"page-123_456"}`)
		case r.Method == "GET" && r.URL.Path == "/page-123_456":
			_, _ = io.WriteString(w, `{"id":"page-123_456","is_published":true,"permalink_url":"https://www.facebook.com/page-123/posts/456"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer platform.Close()
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
	task, err := tasks.Create(ctx, taskengine.CreateTaskInput{Title: "Campaign", Objective: "Publish factual content", OriginalRequest: "Publish Page post", Actor: "owner", Criteria: []taskengine.Criterion{{ID: "AC-1", Description: "Verified post"}}})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := dataStore.OwnerPrincipalID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dataStore.DB.ExecContext(ctx, `INSERT INTO task_research_candidates(id,task_id,owner_principal_id,query,source_kind,doi,title,publisher,source_url,published_date,published_precision,retrieved_at,source_digest,selected) VALUES('source-1',?,?,'topic','crossref','10.1234/test','Study','Publisher','https://doi.org/10.1234/test','2026-09-25','day',?,'digest-1',1)`, task.ID, owner, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := content.SaveContentBrief(ctx, task.ID, product.ContentBrief{Topic: "Research result", Audience: "Readers", Language: "Thai", FacebookPage: "Fixture Page", FreshnessDays: 30}); err != nil {
		t.Fatal(err)
	}
	claim, err := content.CreateContentClaim(ctx, task.ID, product.ContentClaimInput{SourceID: "source-1", Statement: "A supported research finding", EvidenceExcerpt: "The study reports an evidence-based finding."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := content.ReviewContentClaim(ctx, task.ID, claim.ID, "supported", "owner"); err != nil {
		t.Fatal(err)
	}
	draft, err := content.SaveContentDraft(ctx, task.ID, product.ContentDraftInput{Platform: "facebook", Destination: "Fixture Page", Body: "Evidence-based post"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := content.ApproveContentDraft(ctx, task.ID, draft.ID, draft.SubjectHash, "owner"); err != nil {
		t.Fatal(err)
	}
	s := New(nil, nil, nil, nil, nil, nil, nil, nil, dataStore, slog.New(slog.NewTextHandler(io.Discard, nil))).WithProduct(content).WithSocialVault(vault)
	s.socialFacebookBase = platform.URL
	app := httptest.NewServer(s.Handler())
	defer app.Close()
	request := func(method, path, body string, want int) []byte {
		t.Helper()
		req, err := http.NewRequest(method, app.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		payload, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("%s %s = %d, want %d: %s", method, path, resp.StatusCode, want, payload)
		}
		return payload
	}
	var connection socialConnection
	if err := json.Unmarshal(request("POST", "/api/social/connections", `{"platform":"facebook","token":"page-token"}`, 200), &connection); err != nil {
		t.Fatal(err)
	}
	var intent socialPost
	if err := json.Unmarshal(request("POST", "/api/tasks/"+task.ID+"/social/posts", `{"draft_id":"`+draft.ID+`","connection_id":"`+connection.ID+`","privacy_level":""}`, 200), &intent); err != nil {
		t.Fatal(err)
	}
	request("POST", "/api/social/posts/"+intent.ID+"/dispatch", "", 409)
	request("POST", "/api/social/posts/"+intent.ID+"/approve", `{"intent_hash":"wrong"}`, 409)
	request("POST", "/api/social/posts/"+intent.ID+"/approve", `{"intent_hash":"`+intent.IntentHash+`"}`, 200)
	var posted socialPost
	var rejected map[string]any
	if err := json.Unmarshal(request("POST", "/api/social/posts/"+intent.ID+"/dispatch", "", 202), &rejected); err != nil {
		t.Fatal(err)
	}
	if rejected["state"] != "failed" {
		t.Fatalf("definite platform rejection=%+v", rejected)
	}
	request("POST", "/api/social/posts/"+intent.ID+"/dispatch", "", 409)
	var retry socialPost
	if err := json.Unmarshal(request("POST", "/api/social/posts/"+intent.ID+"/retry", "{}", 200), &retry); err != nil {
		t.Fatal(err)
	}
	if retry.State != "review" || retry.RetryOf != intent.ID || retry.ID == intent.ID || retry.IntentHash == intent.IntentHash {
		t.Fatalf("retry record=%+v", retry)
	}
	request("POST", "/api/social/posts/"+retry.ID+"/dispatch", "", 409)
	request("POST", "/api/social/posts/"+retry.ID+"/approve", `{"intent_hash":"`+retry.IntentHash+`"}`, 200)
	if err := json.Unmarshal(request("POST", "/api/social/posts/"+retry.ID+"/dispatch", "", 200), &posted); err != nil {
		t.Fatal(err)
	}
	if posted.State != "published" || posted.RemoteURL != "https://www.facebook.com/page-123/posts/456" {
		t.Fatalf("remote receipt=%+v", posted)
	}
	report, err := tasks.EvaluateCompletion(ctx, task.ID, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	var delivery string
	for _, gate := range report.Gates {
		if gate.ID == "content_delivery" {
			delivery = string(gate.Status)
		}
	}
	if delivery != "pass" {
		t.Fatalf("verified remote publication did not satisfy delivery gate: %s", delivery)
	}
	request("POST", "/api/social/posts/"+retry.ID+"/dispatch", "", 409)
	if creates.Load() != 2 {
		t.Fatalf("remote post created %d times", creates.Load())
	}
	if _, err := content.SaveContentDraft(ctx, task.ID, product.ContentDraftInput{ID: draft.ID, ExpectedRevision: draft.Revision, Platform: "facebook", Destination: "Fixture Page", Body: "Edited post"}); err != nil {
		t.Fatal(err)
	}
	report, err = tasks.EvaluateCompletion(ctx, task.ID, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	for _, gate := range report.Gates {
		if gate.ID == "content_delivery" && gate.Status == "pass" {
			t.Fatal("edited content reused old publication receipt")
		}
	}
	// A newly prepared export for the edited draft must seek a new approval.
	request("POST", "/api/tasks/"+task.ID+"/social/posts", `{"draft_id":"`+draft.ID+`","connection_id":"`+connection.ID+`"}`, 409)
}
