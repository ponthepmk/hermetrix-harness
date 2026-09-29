package product

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hermetrix-harness/internal/taskengine"
)

func TestContentApprovalBindsExactDraftSourceAndDestination(t *testing.T) {
	service, _, dataStore := testProductService(t)
	ctx := context.Background()
	tasks := taskengine.NewService(dataStore)
	task, err := tasks.Create(ctx, taskengine.CreateTaskInput{Title: "Campaign", Objective: "Prepare evidence-based posts", OriginalRequest: "Prepare posts", Actor: "owner",
		Criteria: []taskengine.Criterion{{ID: "AC-1", Description: "Reviewed draft"}}})
	if err != nil {
		t.Fatal(err)
	}
	ownerID, err := dataStore.OwnerPrincipalID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dataStore.DB.ExecContext(ctx, `INSERT INTO task_research_candidates(id,task_id,owner_principal_id,query,source_kind,doi,title,publisher,source_url,published_date,published_precision,retrieved_at,source_digest,selected)
		VALUES('source-1',?,?,'topic','crossref','10.1234/test','Study','Publisher','https://doi.org/10.1234/test','2026-09-25','day',?,'digest-1',1)`, task.ID, ownerID, formatTime(time.Now().UTC()))
	if err != nil {
		t.Fatal(err)
	}
	brief, err := service.SaveContentBrief(ctx, task.ID, ContentBrief{Topic: "Useful research", Audience: "Store owners", Language: "Thai", FacebookPage: "Page A", TikTokAccount: "Account B", FreshnessDays: 30})
	if err != nil || brief.Revision != 1 {
		t.Fatalf("brief=%+v err=%v", brief, err)
	}
	if _, err := service.SaveContentBrief(ctx, task.ID, ContentBrief{Topic: "Stale", Audience: "Owners", Language: "Thai", FacebookPage: "Page A", FreshnessDays: 30}); !errors.Is(err, ErrContentStale) {
		t.Fatalf("stale brief err=%v", err)
	}
	claim, err := service.CreateContentClaim(ctx, task.ID, ContentClaimInput{SourceID: "source-1", Statement: "The paper reports a finding", EvidenceExcerpt: "The authors report the measured finding in the paper."})
	if err != nil {
		t.Fatal(err)
	}
	draft, err := service.SaveContentDraft(ctx, task.ID, ContentDraftInput{Platform: "facebook", Destination: "Page A", Body: "A careful summary with a source."})
	if err != nil {
		t.Fatal(err)
	}
	if draft.ApprovalReady {
		t.Fatal("unreviewed claim allowed approval")
	}
	if _, err := service.SuggestContentDrafts(ctx, task.ID); !errors.Is(err, ErrContentNotReady) {
		t.Fatalf("unreviewed claim produced copy: %v", err)
	}
	if _, err := service.ApproveContentDraft(ctx, task.ID, draft.ID, draft.SubjectHash, "owner"); !errors.Is(err, ErrContentNotReady) {
		t.Fatalf("approval before review err=%v", err)
	}
	if _, err := service.ReviewContentClaim(ctx, task.ID, claim.ID, "conflict", "owner"); err != nil {
		t.Fatal(err)
	}
	workspace, err := service.GetContentWorkspace(ctx, task.ID)
	if err != nil || workspace.Drafts[0].ApprovalReady {
		t.Fatalf("conflicting claim allowed approval: %+v err=%v", workspace, err)
	}
	if _, err := service.ReviewContentClaim(ctx, task.ID, claim.ID, "supported", "owner"); err != nil {
		t.Fatal(err)
	}
	workspace, err = service.GetContentWorkspace(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	draft = workspace.Drafts[0]
	if !draft.ApprovalReady || draft.Approved {
		t.Fatalf("reviewed draft=%+v", draft)
	}
	suggestions, err := service.SuggestContentDrafts(ctx, task.ID)
	if err != nil || len(suggestions) != 2 || suggestions[0].Platform != "facebook" || suggestions[1].Platform != "tiktok" ||
		!strings.Contains(suggestions[0].Body, "https://doi.org/10.1234/test") || !strings.Contains(suggestions[1].Script, claim.Statement) || len(suggestions[0].SourceClaimIDs) != 1 {
		t.Fatalf("source-grounded suggestions=%+v err=%v", suggestions, err)
	}
	approved, err := service.ApproveContentDraft(ctx, task.ID, draft.ID, draft.SubjectHash, "owner")
	if err != nil || !approved.Approved {
		t.Fatalf("approval=%+v err=%v", approved, err)
	}
	if _, err := service.SaveContentDraft(ctx, task.ID, ContentDraftInput{ID: draft.ID, ExpectedRevision: draft.Revision, Platform: "facebook", Destination: "Page A", Body: "Revised text."}); err != nil {
		t.Fatal(err)
	}
	workspace, err = service.GetContentWorkspace(ctx, task.ID)
	if err != nil || workspace.Drafts[0].Approved || workspace.Drafts[0].SubjectHash == draft.SubjectHash {
		t.Fatalf("edit did not invalidate approval: %+v err=%v", workspace.Drafts, err)
	}
	if _, err := service.ApproveContentDraft(ctx, task.ID, draft.ID, draft.SubjectHash, "owner"); !errors.Is(err, ErrContentStale) {
		t.Fatalf("stale approval err=%v", err)
	}
	if _, err := service.SaveContentDraft(ctx, task.ID, ContentDraftInput{Platform: "facebook", Destination: "Different Page", Body: "Wrong destination"}); !errors.Is(err, ErrContentInvalid) {
		t.Fatalf("destination substitution err=%v", err)
	}
	forged, err := service.CreateArtifact(ctx, ArtifactInput{Name: "fake.mp4", Kind: "video", MIMEType: "video/mp4", Content: "not a video", Metadata: map[string]any{"validation": "ffprobe-v1"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SaveContentDraft(ctx, task.ID, ContentDraftInput{Platform: "tiktok", Destination: "Account B", Body: "Video caption", MediaArtifactID: forged.ID, MediaRightsNote: "I created and own this video"}); !errors.Is(err, ErrContentInvalid) {
		t.Fatalf("forged video was accepted: %v", err)
	}
	tiktok, err := service.SaveContentDraft(ctx, task.ID, ContentDraftInput{Platform: "tiktok", Destination: "Account B", Body: "A short caption", Script: "Voiceover", Storyboard: "Three scenes"})
	if err != nil || tiktok.ApprovalReady {
		t.Fatalf("TikTok media gate=%+v err=%v", tiktok, err)
	}
	if _, err := dataStore.DB.ExecContext(ctx, `UPDATE task_research_candidates SET source_digest='digest-2' WHERE id='source-1'`); err != nil {
		t.Fatal(err)
	}
	workspace, err = service.GetContentWorkspace(ctx, task.ID)
	if err != nil || workspace.Claims[0].Current || workspace.Drafts[0].ApprovalReady {
		t.Fatalf("source change did not stale claims: %+v err=%v", workspace, err)
	}
}

func TestConsequentialClaimNeedsTwoCurrentSources(t *testing.T) {
	service, _, dataStore := testProductService(t)
	ctx := context.Background()
	tasks := taskengine.NewService(dataStore)
	task, err := tasks.Create(ctx, taskengine.CreateTaskInput{Title: "Evidence review", Objective: "Prepare a checked post", OriginalRequest: "Article post", Actor: "owner",
		Criteria: []taskengine.Criterion{{ID: "AC-1", Description: "Checked post"}}})
	if err != nil {
		t.Fatal(err)
	}
	ownerID, err := dataStore.OwnerPrincipalID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []struct{ id, doi, digest string }{{"source-a", "10.1234/a", "digest-a"}, {"source-b", "10.1234/b", "digest-b"}} {
		_, err = dataStore.DB.ExecContext(ctx, `INSERT INTO task_research_candidates(id,task_id,owner_principal_id,query,source_kind,doi,title,publisher,source_url,published_date,published_precision,retrieved_at,source_digest,selected)
			VALUES(?,?,?,'topic','crossref',?,'Study','Publisher',?,'2026-09-25','day',?,?,1)`, source.id, task.ID, ownerID, source.doi, "https://doi.org/"+source.doi, formatTime(time.Now().UTC()), source.digest)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.SaveContentBrief(ctx, task.ID, ContentBrief{Topic: "Research", Audience: "Readers", Language: "Thai", FacebookPage: "Page A", FreshnessDays: 30}); err != nil {
		t.Fatal(err)
	}
	input := ContentClaimInput{SourceID: "source-a", Statement: "A material result was observed", EvidenceExcerpt: "The first article reports the measured result.", Consequential: true,
		CorroboratingSourceID: "source-a", CorroboratingExcerpt: "The second report confirms the measured result."}
	if _, err := service.CreateContentClaim(ctx, task.ID, input); !errors.Is(err, ErrContentInvalid) {
		t.Fatalf("same article accepted as two sources: %v", err)
	}
	input.CorroboratingSourceID = "source-b"
	claim, err := service.CreateContentClaim(ctx, task.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if !claim.Consequential || claim.CorroboratingSourceDigest != "digest-b" {
		t.Fatalf("second source not bound: %+v", claim)
	}
	if _, err := service.ReviewContentClaim(ctx, task.ID, claim.ID, "supported", "owner"); err != nil {
		t.Fatal(err)
	}
	suggestions, err := service.SuggestContentDrafts(ctx, task.ID)
	if err != nil || len(suggestions) != 1 || !strings.Contains(suggestions[0].Body, "https://doi.org/10.1234/a") || !strings.Contains(suggestions[0].Body, "https://doi.org/10.1234/b") {
		t.Fatalf("two-source copy omitted evidence: %+v err=%v", suggestions, err)
	}
	draft, err := service.SaveContentDraft(ctx, task.ID, ContentDraftInput{Platform: "facebook", Destination: "Page A", Body: "A careful two-source summary."})
	if err != nil || !draft.ApprovalReady {
		t.Fatalf("two-source draft=%+v err=%v", draft, err)
	}
	if _, err := service.ApproveContentDraft(ctx, task.ID, draft.ID, draft.SubjectHash, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := dataStore.DB.ExecContext(ctx, `UPDATE task_research_candidates SET selected=0 WHERE id='source-b'`); err != nil {
		t.Fatal(err)
	}
	workspace, err := service.GetContentWorkspace(ctx, task.ID)
	if err != nil || workspace.Claims[0].Current || workspace.Drafts[0].ApprovalReady || workspace.Drafts[0].Approved {
		t.Fatalf("second source removal did not stale approval: %+v err=%v", workspace, err)
	}
}

func TestVideoRightsNoteIsRequiredAndBoundToContentApproval(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is needed for a real video fixture")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is needed for video validation")
	}
	path := filepath.Join(t.TempDir(), "rights.mp4")
	if output, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "color=c=blue:s=64x64:r=10", "-t", "1", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-y", path).CombinedOutput(); err != nil {
		t.Fatalf("create video: %v %s", err, output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service, _, dataStore := testProductService(t)
	ctx := context.Background()
	tasks := taskengine.NewService(dataStore)
	task, err := tasks.Create(ctx, taskengine.CreateTaskInput{Title: "Video rights", Objective: "Review a clip", OriginalRequest: "Review a clip", Actor: "owner", Criteria: []taskengine.Criterion{{ID: "AC-1", Description: "Approved source-grounded clip"}}})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := dataStore.OwnerPrincipalID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dataStore.DB.ExecContext(ctx, `INSERT INTO task_research_candidates(id,task_id,owner_principal_id,query,source_kind,doi,title,publisher,source_url,published_date,published_precision,retrieved_at,source_digest,selected)
		VALUES('rights-source',?,?,'video','crossref','10.1234/rights','Study','Publisher','https://doi.org/10.1234/rights','2026-09-25','day',?,'rights-digest',1)`, task.ID, owner, formatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SaveContentBrief(ctx, task.ID, ContentBrief{Topic: "Video evidence", Audience: "Thai readers", Language: "Thai", TikTokAccount: "Example Account", FreshnessDays: 30}); err != nil {
		t.Fatal(err)
	}
	claim, err := service.CreateContentClaim(ctx, task.ID, ContentClaimInput{SourceID: "rights-source", Statement: "The study reports a measured finding", EvidenceExcerpt: "The authors report the measured finding in the paper."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReviewContentClaim(ctx, task.ID, claim.ID, "supported", "owner"); err != nil {
		t.Fatal(err)
	}
	video, err := service.UploadVideo(ctx, VideoUploadInput{Name: "rights.mp4", Reader: bytes.NewReader(data)})
	if err != nil {
		t.Fatal(err)
	}
	draft, err := service.SaveContentDraft(ctx, task.ID, ContentDraftInput{Platform: "tiktok", Destination: "Example Account", Body: "Reviewed finding", Script: "Speak", Storyboard: "Show", MediaArtifactID: video.ID})
	if err != nil || draft.ApprovalReady {
		t.Fatalf("missing rights note did not block approval: %+v err=%v", draft, err)
	}
	draft, err = service.SaveContentDraft(ctx, task.ID, ContentDraftInput{ID: draft.ID, ExpectedRevision: draft.Revision, Platform: "tiktok", Destination: "Example Account", Body: "Reviewed finding", Script: "Speak", Storyboard: "Show", MediaArtifactID: video.ID, MediaRightsNote: "I created this clip and own all its visual elements"})
	if err != nil || !draft.ApprovalReady {
		t.Fatalf("documented rights did not enable review: %+v err=%v", draft, err)
	}
	if _, err := service.ApproveContentDraft(ctx, task.ID, draft.ID, draft.SubjectHash, "owner"); err != nil {
		t.Fatal(err)
	}
	changed, err := service.SaveContentDraft(ctx, task.ID, ContentDraftInput{ID: draft.ID, ExpectedRevision: draft.Revision, Platform: "tiktok", Destination: "Example Account", Body: "Reviewed finding", Script: "Speak", Storyboard: "Show", MediaArtifactID: video.ID, MediaRightsNote: "Rights need another review before posting"})
	if err != nil || changed.Approved || changed.SubjectHash == draft.SubjectHash {
		t.Fatalf("rights edit kept stale approval: %+v err=%v", changed, err)
	}
}
