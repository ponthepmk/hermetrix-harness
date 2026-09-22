package product

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 3))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var body bytes.Buffer
	if err := png.Encode(&body, img); err != nil {
		t.Fatal(err)
	}
	return body.Bytes()
}

func TestMediaUploadAndDurableImageInspection(t *testing.T) {
	service, _, _ := testProductService(t)
	ctx := context.Background()
	artifact, err := service.UploadMedia(ctx, MediaUploadInput{Name: "proof.png", MIMEType: "image/png", Data: testPNG(t)})
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Visibility != "private" || artifact.ExportPolicy != "deny" || artifact.MIMEType != "image/png" {
		t.Fatalf("upload policy=%+v", artifact)
	}
	job, err := service.StartMediaJob(ctx, MediaJobInput{SourceArtifactID: artifact.ID, ProcessorKind: "image_inspect",
		OperationID: "inspect-proof", IdempotencyKey: "client-1", Options: map[string]any{"question": "dimensions"}})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for job.State == "queued" || job.State == "running" {
		if time.Now().After(deadline) {
			t.Fatal("media job did not finish")
		}
		time.Sleep(10 * time.Millisecond)
		job, err = service.GetMediaJob(ctx, job.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if job.State != "completed" || job.Progress != 1 || len(job.ResultArtifactIDs) != 1 {
		t.Fatalf("job=%+v", job)
	}
	derived, _, err := service.GetArtifact(ctx, job.ResultArtifactIDs[0])
	if err != nil || derived.Kind != "image_evidence" || len(derived.SourceLineage) != 1 || derived.SourceLineage[0] != artifact.ID {
		t.Fatalf("derived=%+v err=%v", derived, err)
	}
	retry, err := service.StartMediaJob(ctx, MediaJobInput{SourceArtifactID: artifact.ID, ProcessorKind: "image_inspect",
		OperationID: "inspect-proof", IdempotencyKey: "client-1", Options: map[string]any{"question": "dimensions"}})
	if err != nil || retry.ID != job.ID {
		t.Fatalf("idempotent retry=%+v err=%v", retry, err)
	}
	_, err = service.StartMediaJob(ctx, MediaJobInput{SourceArtifactID: artifact.ID, ProcessorKind: "image_inspect",
		OperationID: "inspect-proof", IdempotencyKey: "client-1", Options: map[string]any{"question": "different"}})
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflict err=%v", err)
	}
}

func TestMediaRejectsForgedMimeAndUnavailableProcessor(t *testing.T) {
	service, _, _ := testProductService(t)
	ctx := context.Background()
	if _, err := service.UploadMedia(ctx, MediaUploadInput{Name: "fake.jpg", MIMEType: "image/jpeg", Data: testPNG(t)}); err == nil {
		t.Fatal("forged MIME was accepted")
	}
	artifact, err := service.UploadMedia(ctx, MediaUploadInput{Name: "proof.png", Data: testPNG(t)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.StartMediaJob(ctx, MediaJobInput{SourceArtifactID: artifact.ID, ProcessorKind: "audio_transcribe",
		OperationID: "stt", IdempotencyKey: "one"})
	if !errors.Is(err, ErrMediaProcessorUnsupported) {
		t.Fatalf("unsupported processor err=%v", err)
	}
}
