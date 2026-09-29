package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func testVideoFixture(t *testing.T) []byte {
	t.Helper()
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is required for video validation")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is required to create a test video")
	}
	path := filepath.Join(t.TempDir(), "sample.mp4")
	if output, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "color=c=blue:s=64x64:r=10",
		"-t", "1", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-movflags", "+faststart", "-y", path).CombinedOutput(); err != nil {
		t.Fatalf("create fixture: %v %s", err, output)
	}
	video, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return video
}

func TestVideoUploadStreamsRangesAndRejectsCorruptMedia(t *testing.T) {
	video := testVideoFixture(t)
	server := testHTTPServer(t)
	upload := func(name string, body []byte) (*http.Response, error) {
		var payload bytes.Buffer
		form := multipart.NewWriter(&payload)
		part, err := form.CreateFormFile("file", name)
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(body); err != nil {
			return nil, err
		}
		if err := form.Close(); err != nil {
			return nil, err
		}
		return http.Post(server.URL+"/api/media/videos", form.FormDataContentType(), &payload)
	}
	created, err := upload("sample.mp4", video)
	if err != nil {
		t.Fatal(err)
	}
	defer created.Body.Close()
	if created.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(created.Body)
		t.Fatalf("upload status=%d body=%s", created.StatusCode, body)
	}
	var artifact struct {
		ID       string         `json:"id"`
		MIMEType string         `json:"mime_type"`
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.NewDecoder(created.Body).Decode(&artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.MIMEType != "video/mp4" || artifact.Metadata["duration_seconds"] == nil {
		t.Fatalf("video metadata=%+v", artifact)
	}
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/api/artifacts/"+artifact.ID+"/stream", nil)
	request.Header.Set("Range", "bytes=10-29")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	chunk, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusPartialContent || !bytes.Equal(chunk, video[10:30]) ||
		response.Header.Get("Accept-Ranges") != "bytes" || !strings.HasPrefix(response.Header.Get("Content-Range"), "bytes 10-29/") {
		t.Fatalf("range status=%d headers=%v bytes=%d", response.StatusCode, response.Header, len(chunk))
	}
	bad, err := upload("broken.mp4", []byte("not a video"))
	if err != nil {
		t.Fatal(err)
	}
	defer bad.Body.Close()
	if bad.StatusCode < 400 {
		t.Fatalf("corrupt upload status=%d", bad.StatusCode)
	}
}

func TestVideoArtifactPlaysAndSeeksInARealBrowser(t *testing.T) {
	chrome := browserExecutableForE2E()
	if chrome == "" {
		t.Skip("Chrome is not installed")
	}
	video := testVideoFixture(t)
	server := testHTTPServer(t)
	var payload bytes.Buffer
	form := multipart.NewWriter(&payload)
	part, err := form.CreateFormFile("file", "sample.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(video); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(server.URL+"/api/media/videos", form.FormDataContentType(), &payload)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("video upload status=%d", response.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	debugURL, stop, err := startE2EChrome(ctx, chrome, filepath.Join(t.TempDir(), "chrome-profile"))
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	target, err := createE2ETarget(ctx, debugURL, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	connection, _, err := websocket.DefaultDialer.DialContext(ctx, target.WebSocketDebuggerURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	client := &e2eCDPClient{connection: connection}
	if err := client.call(ctx, "Runtime.enable", map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `typeof switchTab === 'function' && document.querySelector('#view-artifacts') ? 'ready' : ''`, "ready")
	if _, err := client.evaluate(ctx, `switchTab('artifacts'); 'artifacts'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#view-artifacts video.artifact-video')?.readyState >= 1 ? 'metadata-loaded' : ''`, "metadata-loaded")
	if _, err := client.evaluate(ctx, `document.querySelector('#view-artifacts video.artifact-video').currentTime=0.5; 'seek-requested'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `(() => { const v=document.querySelector('#view-artifacts video.artifact-video'); return v && !v.seeking && v.currentTime>=0.4 ? 'seeked' : '' })()`, "seeked")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-video-evidence]').click(); 'evidence-open'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-video-frame-start]') ? 'frame-start' : ''`, "frame-start")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-video-frame-start]').click(); 'frame-requested'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#actionDialog')?.open ? 'frame-review' : ''`, "frame-review")
	if _, err := client.evaluate(ctx, `document.querySelector('#actionConfirm').click(); 'frame-confirmed'`); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 30; attempt++ {
		value, err := client.evaluate(ctx, `document.querySelectorAll('.video-frame-results figure').length === 3 ? 'three-frames' : ''`)
		if err != nil {
			t.Fatal(err)
		}
		if value == "three-frames" {
			break
		}
		if _, err := client.evaluate(ctx, `document.querySelector('[data-video-job-refresh]')?.click(); 'refreshed'`); err != nil {
			t.Fatal(err)
		}
		time.Sleep(250 * time.Millisecond)
	}
	waitBrowserValue(t, ctx, client, `document.querySelectorAll('.video-frame-results figure').length === 3 ? 'three-frames' : ''`, "three-frames")
	waitBrowserValue(t, ctx, client, `document.querySelector('[data-video-transcribe]') ? 'transcribe-ready' : ''`, "transcribe-ready")
	if _, err := client.evaluate(ctx, `document.querySelector('[data-video-transcribe]').click(); 'transcribe-requested'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#actionDialog')?.open ? 'transcribe-review' : ''`, "transcribe-review")
	if _, err := client.evaluate(ctx, `document.querySelector('#actionCancel').click(); 'transcribe-cancelled'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#actionDialog')?.open ? '' : 'closed'`, "closed")
}

func TestVideoFrameEvidenceUsesRealFFmpegAndPersistsTimestamps(t *testing.T) {
	video := testVideoFixture(t)
	server := testHTTPServer(t)
	var payload bytes.Buffer
	form := multipart.NewWriter(&payload)
	part, err := form.CreateFormFile("file", "frames.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(video); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(server.URL+"/api/media/videos", form.FormDataContentType(), &payload)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("video upload status=%d body=%s", response.StatusCode, body)
	}
	var source struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&source); err != nil {
		t.Fatal(err)
	}
	var job struct {
		ID                string   `json:"id"`
		State             string   `json:"state"`
		ResultArtifactIDs []string `json:"result_artifact_ids"`
		Error             string   `json:"error"`
	}
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/media/jobs", http.MethodPost, map[string]any{
		"source_artifact_id": source.ID, "processor_kind": "video_extract", "operation_id": "frame-fixture-op", "idempotency_key": "frame-fixture-key",
	}, http.StatusAccepted), &job); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if err := json.Unmarshal(requestJSON(t, server.URL+"/api/media/jobs/"+job.ID, http.MethodGet, nil, http.StatusOK), &job); err != nil {
			t.Fatal(err)
		}
		if job.State == "completed" {
			break
		}
		if job.State == "failed" || job.State == "cancelled" {
			t.Fatalf("frame extraction state=%s error=%s", job.State, job.Error)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if job.State != "completed" || len(job.ResultArtifactIDs) != 3 {
		t.Fatalf("frame job=%+v", job)
	}
	var artifacts []struct {
		ID       string         `json:"id"`
		Kind     string         `json:"kind"`
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/artifacts", http.MethodGet, nil, http.StatusOK), &artifacts); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, artifact := range artifacts {
		for _, id := range job.ResultArtifactIDs {
			if artifact.ID == id {
				if artifact.Kind != "video_frame" || artifact.Metadata["source_artifact_id"] != source.ID || artifact.Metadata["frame_time_seconds"] == nil {
					t.Fatalf("frame metadata=%+v", artifact)
				}
				seen[id] = true
			}
		}
	}
	if len(seen) != 3 {
		t.Fatalf("only %d timestamped frames are visible", len(seen))
	}
}
