package product

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestParseWhisperTranscriptRejectsInvalidTimeRanges(t *testing.T) {
	data := []byte(`{"result":{"language":"en"},"transcription":[{"offsets":{"from":100,"to":900},"text":" hello "}]}`)
	language, segments, err := parseWhisperTranscript(data, 1)
	if err != nil || language != "en" || len(segments) != 1 || segments[0].StartMS != 100 || segments[0].Text != "hello" {
		t.Fatalf("parsed transcript=%q %+v err=%v", language, segments, err)
	}
	if _, _, err := parseWhisperTranscript([]byte(`{"transcription":[{"offsets":{"from":0,"to":99000},"text":"forged"}]}`), 1); err == nil {
		t.Fatal("out of bounds transcript timestamp was accepted")
	}
}

func TestVideoTranscriptionUsesLocalWhisperAndPersistsTimedEvidence(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("installed local Whisper fixture is Windows specific")
	}
	fixture := `C:\Tools\whisper.cpp\samples\jfk.wav`
	if _, err := os.Stat(fixture); err != nil {
		t.Skip("local speech fixture is unavailable")
	}
	if _, err := discoverWhisperRuntime(); err != nil {
		t.Skipf("local Whisper runtime is unavailable: %v", err)
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is unavailable")
	}
	path := filepath.Join(t.TempDir(), "speech.mp4")
	output, err := exec.Command(ffmpeg, "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=1", "-i", fixture,
		"-shortest", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-movflags", "+faststart", "-y", path).CombinedOutput()
	if err != nil {
		t.Fatalf("create speech video: %v %s", err, output)
	}
	service, _, _ := testProductService(t)
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	ctx := context.Background()
	source, err := service.UploadVideo(ctx, VideoUploadInput{Name: "speech.mp4", Reader: file})
	if err != nil {
		t.Fatal(err)
	}
	job, err := service.StartMediaJob(ctx, MediaJobInput{SourceArtifactID: source.ID, ProcessorKind: "audio_transcribe", OperationID: "speech", IdempotencyKey: "once"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(90 * time.Second)
	for job.State == "queued" || job.State == "running" {
		if time.Now().After(deadline) {
			t.Fatal("local transcription did not finish")
		}
		time.Sleep(100 * time.Millisecond)
		job, err = service.GetMediaJob(ctx, job.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if job.State != "completed" || len(job.ResultArtifactIDs) != 1 {
		t.Fatalf("transcription job=%+v", job)
	}
	artifact, data, err := service.GetArtifact(ctx, job.ResultArtifactIDs[0])
	if err != nil || artifact.Kind != "video_transcript" || len(artifact.SourceLineage) != 1 || artifact.SourceLineage[0] != source.ID {
		t.Fatalf("derived artifact=%+v err=%v", artifact, err)
	}
	var result struct {
		SourceHash string              `json:"source_hash"`
		Segments   []transcriptSegment `json:"segments"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.SourceHash != source.Checksum || len(result.Segments) == 0 || result.Segments[0].StartMS < 0 ||
		!strings.Contains(strings.ToLower(result.Segments[0].Text), "fellow americans") {
		t.Fatalf("timed transcript=%+v", result)
	}
}
