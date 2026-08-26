package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hermetrix-harness/internal/embedding"
)

// conceptEmbedder is a deterministic stand-in for a sentence model: it maps
// text to a point by which concepts it mentions, so two ways of saying the same
// thing land in the same place and unrelated text does not.
//
// A real model is exercised separately; a fake is what lets these tests assert
// the wiring without a 2 GB download or a network call, and lets them state
// exactly which similarity they depend on.
type conceptEmbedder struct{ calls int }

var conceptVocabulary = []struct {
	name  string
	words []string
}{
	{"batch", []string{"batch size", "จำนวนตัวอย่างต่อรอบ", "ขนาดชุดข้อมูล"}},
	{"rounding", []string{"ปัดเศษ", "ทำเป็นจำนวนเต็ม", "round"}},
	{"deploy", []string{"แผนงาน", "นำขึ้นระบบ", "deploy"}},
}

func (e *conceptEmbedder) Revision() string { return "embed:concept-fake-v1" }
func (e *conceptEmbedder) Dimensions() int  { return len(conceptVocabulary) }

func (e *conceptEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.calls++
	out := make([][]float32, len(texts))
	for index, text := range texts {
		vector := make([]float32, len(conceptVocabulary))
		for concept, entry := range conceptVocabulary {
			for _, word := range entry.words {
				if containsFold(text, word) {
					vector[concept] = 1
					break
				}
			}
		}
		out[index] = embedding.Normalise(vector)
	}
	return out, nil
}

func containsFold(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		indexFold(haystack, needle) >= 0
}

func indexFold(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// TestSemanticSearchCrossesAParaphrase closes O-44's wiring.
//
// On the phrasing corpus the model searched 18 times out of 19 when it could
// not answer, and lexical search found the fact 3 times: it queried in the
// question's words while the fact was written in different ones -- the same
// mismatch that made the compactor drop it. This is the case, reduced to one
// event and one query that share no words.
func TestSemanticSearchCrossesAParaphrase(t *testing.T) {
	service, provider, cleanup := testAgentService(t, httptest.NewServer(http.NotFoundHandler()))
	defer cleanup()
	ctx := context.Background()
	session := seedSemanticSession(t, service, provider.ID)

	const query = "batch size ที่ทีมเลือกใช้คือเท่าไหร่"
	// Premise: lexically the query reaches nothing, or this test would pass
	// without semantic retrieval doing any work.
	events, err := service.ListEvents(ctx, session)
	if err != nil {
		t.Fatal(err)
	}
	if lexical := searchEvents(events, query, 5, nil); len(lexical) > 0 {
		t.Fatalf("premise broken: lexical search already finds it: %+v", lexical[0].EventID)
	}

	service.SetEmbedder(&conceptEmbedder{})
	if _, err := service.embedNewEvents(ctx, session); err != nil {
		t.Fatal(err)
	}
	semantic, err := service.semanticMatches(ctx, session, query)
	if err != nil {
		t.Fatal(err)
	}
	results := searchEvents(events, query, 5, semantic)
	if len(results) == 0 {
		t.Fatal("semantic search did not cross the paraphrase")
	}
	if results[0].EventID != "event_far" {
		t.Fatalf("the paraphrase was not ranked first: %+v", results)
	}
}

// Semantic retrieval is added to lexical, not substituted for it. In the same
// measured run, where the wording matched, lexical was perfect -- far/head
// searched 7 and found 7 -- and an exact identifier is something a substring
// finds and a vector approximates.
func TestSemanticDoesNotDisplaceAnExactMatch(t *testing.T) {
	service, provider, cleanup := testAgentService(t, httptest.NewServer(http.NotFoundHandler()))
	defer cleanup()
	ctx := context.Background()
	session := seedSemanticSession(t, service, provider.ID)
	service.SetEmbedder(&conceptEmbedder{})
	if _, err := service.embedNewEvents(ctx, session); err != nil {
		t.Fatal(err)
	}
	events, err := service.ListEvents(ctx, session)
	if err != nil {
		t.Fatal(err)
	}
	semantic, err := service.semanticMatches(ctx, session, "ROUND_HALF_UP_4096")
	if err != nil {
		t.Fatal(err)
	}
	results := searchEvents(events, "ROUND_HALF_UP_4096", 5, semantic)
	if len(results) == 0 || results[0].EventID != "event_exact" {
		t.Fatalf("an exact identifier was outranked by a semantic neighbour: %+v", results)
	}
}

// With no embedder configured everything still works, lexically. Hermetrix is
// local-first and an embedder is a second model to run; a workspace without one
// is a supported configuration, not a degraded one.
func TestSearchWorksWithNoEmbedder(t *testing.T) {
	service, provider, cleanup := testAgentService(t, httptest.NewServer(http.NotFoundHandler()))
	defer cleanup()
	ctx := context.Background()
	session := seedSemanticSession(t, service, provider.ID)

	if _, err := service.embedNewEvents(ctx, session); !errors.Is(err, embedding.ErrNoEmbedder) {
		t.Fatalf("expected ErrNoEmbedder, got %v", err)
	}
	if _, err := service.semanticMatches(ctx, session, "anything"); !errors.Is(err, embedding.ErrNoEmbedder) {
		t.Fatalf("expected ErrNoEmbedder, got %v", err)
	}
	events, err := service.ListEvents(ctx, session)
	if err != nil {
		t.Fatal(err)
	}
	if results := searchEvents(events, "ROUND_HALF_UP_4096", 5, nil); len(results) == 0 {
		t.Fatal("lexical search stopped working without an embedder")
	}
}

// A vector written by one model says nothing about a query embedded by
// another: the coordinates mean different things. Storing the revision and
// filtering on it is what keeps two geometries from being compared as if they
// were one.
func TestVectorsFromAnotherModelAreNotUsed(t *testing.T) {
	service, provider, cleanup := testAgentService(t, httptest.NewServer(http.NotFoundHandler()))
	defer cleanup()
	ctx := context.Background()
	session := seedSemanticSession(t, service, provider.ID)
	service.SetEmbedder(&conceptEmbedder{})
	if _, err := service.embedNewEvents(ctx, session); err != nil {
		t.Fatal(err)
	}
	if matches, err := service.semanticMatches(ctx, session, "batch size"); err != nil || len(matches) == 0 {
		t.Fatalf("premise broken: matches=%d err=%v", len(matches), err)
	}
	// The operator swaps the model. The stored vectors are still there.
	service.SetEmbedder(&renamedEmbedder{conceptEmbedder{}})
	matches, err := service.semanticMatches(ctx, session, "batch size")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("vectors from the previous model were compared against the new one: %d", len(matches))
	}
}

type renamedEmbedder struct{ conceptEmbedder }

func (renamedEmbedder) Revision() string { return "embed:something-else-v1" }

// seedSemanticSession writes one event stating a fact in words a question would
// not use, and one containing an exact identifier.
func seedSemanticSession(t *testing.T, service *Service, providerID string) string {
	t.Helper()
	ctx := context.Background()
	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: providerID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	insert := func(id, content string, sequence int) {
		if _, err := service.store.DB.ExecContext(ctx, `INSERT INTO agent_events(id,session_id,turn_id,
          sequence,event_kind,role,content,metadata_json,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			id, session.ID, "t1", sequence, "message", "assistant", content, "{}", formatTime(now)); err != nil {
			t.Fatal(err)
		}
	}
	insert("event_far", "สรุปว่าจำนวนตัวอย่างต่อรอบประมวลผลที่เคาะกันไว้คือ 4096 หลังลองมาสามครั้ง", 1)
	insert("event_exact", "ใช้ ROUND_HALF_UP_4096 ในการปัดเศษตามที่ตกลง", 2)
	insert("event_noise", "เรื่องอื่นที่ไม่เกี่ยวข้องกับงานนี้เลย", 3)
	return session.ID
}

// A fixed cosine threshold does not transfer between queries. Measured with
// bge-m3 on real Thai, a question about batch size scored 0.480 against its own
// paraphrase and 0.419 against unrelated prose, while a question about a plan
// id scored 0.604 and 0.471 -- so any constant that keeps the first paraphrase
// admits the second question's noise. Selection is relative to the session's
// own baseline for that reason.
func TestSelectionIsRelativeToTheSessionBaseline(t *testing.T) {
	service, provider, cleanup := testAgentService(t, httptest.NewServer(http.NotFoundHandler()))
	defer cleanup()
	ctx := context.Background()
	session := seedSemanticSession(t, service, provider.ID)
	service.SetEmbedder(&conceptEmbedder{})
	if _, err := service.embedNewEvents(ctx, session); err != nil {
		t.Fatal(err)
	}
	matches, err := service.semanticMatches(ctx, session, "batch size ที่ทีมเลือกใช้")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := matches["event_far"]; !ok {
		t.Fatalf("the paraphrase was not selected: %+v", matches)
	}
	if _, ok := matches["event_noise"]; ok {
		t.Fatalf("unrelated prose was selected alongside it: %+v", matches)
	}
	// A query about nothing in the session must not drag the top of a narrow
	// band in with it.
	empty, err := service.semanticMatches(ctx, session, "เรื่องที่ไม่เคยคุยกันเลยในเซสชันนี้")
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) > SemanticTopK {
		t.Fatalf("an unrelated query selected %d events", len(empty))
	}
}
