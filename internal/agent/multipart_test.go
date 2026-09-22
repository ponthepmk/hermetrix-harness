package agent

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"testing"
	"time"

	"hermetrix-harness/internal/identity"
)

func TestMultipartEventPersistsReferencesAndMaterializesOwnedImage(t *testing.T) {
	service, profile, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	ctx := context.Background()
	projectID := createTestProject(t, service, t.TempDir())
	session, err := service.CreateSession(ctx, CreateSessionInput{Title: "vision", ProviderID: profile.ID,
		ProjectID: projectID, ContextProfile: "compact-32k"})
	if err != nil {
		t.Fatal(err)
	}
	var imageBytes bytes.Buffer
	if err = png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	data := imageBytes.Bytes()
	ref, err := service.store.Blobs.Put(data)
	if err != nil {
		t.Fatal(err)
	}
	ownerID, err := service.store.OwnerPrincipalID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	artifactID := identity.New("artifact")
	if _, err = service.store.DB.ExecContext(ctx, `INSERT INTO artifacts(id,project_id,name,kind,mime_type,blob_ref,byte_size,checksum,metadata_json,created_at,owner_principal_id)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, artifactID, projectID, "pixel.png", "source_image", "image/png", ref, len(data), ref, `{}`,
		time.Now().UTC().Format(time.RFC3339Nano), ownerID); err != nil {
		t.Fatal(err)
	}
	input := []EventPart{{Kind: "text", Text: "describe this"}, {Kind: "image", ArtifactID: artifactID}}
	parts, goal, err := service.validateTurnParts(ctx, session, input)
	if err != nil || goal != "describe this\n[image:"+artifactID+"]" {
		t.Fatalf("goal=%q parts=%+v err=%v", goal, parts, err)
	}
	event, err := service.acquireTurn(ctx, session, profile, identity.New("turn"), "", parts)
	if err != nil {
		t.Fatal(err)
	}
	materialized, err := service.materializeTurnParts(ctx, session.ID, event.TurnID)
	if err != nil {
		t.Fatal(err)
	}
	if len(materialized) != 2 || materialized[1].ArtifactID != artifactID || !bytes.Equal(materialized[1].Data, data) {
		t.Fatalf("materialized=%+v", materialized)
	}
	events, err := service.ListEvents(ctx, session.ID)
	if err != nil || len(events) != 1 || len(events[0].Parts) != 2 || events[0].Content != "" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	if _, _, detected, err := boundedImageInfo(data); err != nil || detected != "image/png" {
		t.Fatalf("detected=%s err=%v", detected, err)
	}
}
