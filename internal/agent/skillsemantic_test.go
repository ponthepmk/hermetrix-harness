package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The catalog these tests use is the shape R-14 was measured on: Thai users,
// English Skill summaries. Nothing in it shares a trigram with a Thai goal.
func englishSkillCatalog() []SessionSkillBinding {
	return []SessionSkillBinding{
		{SkillID: "s1", VersionID: "v1", CanonicalName: "satang-rounding",
			Summary: "round thai money amounts half up in satang"},
		{SkillID: "s2", VersionID: "v2", CanonicalName: "invoice-numbering",
			Summary: "format thai invoice numbers as INV plus five digits"},
		{SkillID: "s3", VersionID: "v3", CanonicalName: "release-checklist",
			Summary: "deploy the service to production safely"},
	}
}

// TestASemanticGoalReachesACatalogInAnotherLanguage closes R-14.
//
// The measured failure: "ปัดเศษเงินบาทเป็นจำนวนเต็มสตางค์" is a literal
// statement of what the rounding Skill does and retrieved nothing, while the
// English translation of the same sentence retrieved it first. The scorer had
// no trigram to share, so the turn was recorded as a turn with no relevant
// Skill -- a blind spot reporting itself as an absence.
func TestASemanticGoalReachesACatalogInAnotherLanguage(t *testing.T) {
	service, _, cleanup := testAgentService(t, httptest.NewServer(http.NotFoundHandler()))
	defer cleanup()
	ctx := context.Background()
	catalog := englishSkillCatalog()
	const goal = "ปัดเศษเงินบาทเป็นจำนวนเต็มสตางค์"

	// Premise: without the embedder this goal reaches nothing, or the test would
	// pass with the semantic path deleted.
	if lexical := selectSkillBindings(goal, catalog); len(lexical) != 0 {
		t.Fatalf("premise broken: the lexical scorer already retrieves %+v", lexical)
	}

	service.SetEmbedder(&conceptEmbedder{})
	semantic := service.skillRelevance(ctx, goal, catalog)
	if len(semantic) == 0 {
		t.Fatal("no Skill earned a semantic bonus")
	}
	selected := rankSkillBindings(goal, catalog, semantic)
	if len(selected) == 0 {
		t.Fatal("the Thai goal still reaches nothing")
	}
	if selected[0].SkillID != "s1" {
		t.Fatalf("ranked %s first, want the rounding Skill", selected[0].SkillID)
	}
}

// A goal about something the catalog does not cover must still retrieve
// nothing. Selection is relative to the catalog's own noise floor precisely so
// that an unrelated goal, where every entry sits on the same band, produces no
// winner -- a scorer that always returns its best match would fill the contract
// with a Skill the turn has no use for.
func TestAnUnrelatedGoalEarnsNoSemanticBonus(t *testing.T) {
	service, _, cleanup := testAgentService(t, httptest.NewServer(http.NotFoundHandler()))
	defer cleanup()
	service.SetEmbedder(&conceptEmbedder{})
	catalog := englishSkillCatalog()
	const goal = "ขนาดชุดข้อมูล ที่ทีมเลือกใช้คือเท่าไหร่"
	if semantic := service.skillRelevance(context.Background(), goal, catalog); len(semantic) != 0 {
		t.Fatalf("an unrelated goal earned %+v", semantic)
	}
	if selected := rankSkillBindings(goal, catalog, nil); len(selected) != 0 {
		t.Fatalf("an unrelated goal retrieved %+v", selected)
	}
}

// Semantic ranking is added to the lexical score, not substituted for it. A
// canonical name is a substring match and a vector only approximates one, so a
// user who names the Skill they want must still get it.
func TestSemanticDoesNotDisplaceANamedSkill(t *testing.T) {
	service, _, cleanup := testAgentService(t, httptest.NewServer(http.NotFoundHandler()))
	defer cleanup()
	service.SetEmbedder(&conceptEmbedder{})
	catalog := englishSkillCatalog()
	// The goal names one Skill and describes another's subject.
	const goal = "invoice numbering, and round the amounts too"
	semantic := service.skillRelevance(context.Background(), goal, catalog)
	selected := rankSkillBindings(goal, catalog, semantic)
	if len(selected) == 0 || selected[0].SkillID != "s2" {
		t.Fatalf("ranked %+v; the named Skill lost to a semantic neighbour", selected)
	}
}

// countingEmbedder records how many texts each batch carried, which is how the
// cache is observed: a warm catalog costs one text per call, the goal.
type countingEmbedder struct {
	conceptEmbedder
	batches []int
}

func (e *countingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	e.batches = append(e.batches, len(texts))
	return e.conceptEmbedder.Embed(ctx, texts)
}

// The catalog is embedded once and read back after that. Without the cache
// every turn would re-embed every Skill in the contract, which turns a fixed
// startup cost into a per-turn one.
func TestTheCatalogIsEmbeddedOnce(t *testing.T) {
	service, _, cleanup := testAgentService(t, httptest.NewServer(http.NotFoundHandler()))
	defer cleanup()
	ctx := context.Background()
	embedder := &countingEmbedder{}
	service.SetEmbedder(embedder)
	catalog := englishSkillCatalog()
	service.skillRelevance(ctx, "ปัดเศษเงินบาท", catalog)
	service.skillRelevance(ctx, "ปัดเศษสตางค์", catalog)
	if len(embedder.batches) != 2 {
		t.Fatalf("batches %v, want one call each", embedder.batches)
	}
	if embedder.batches[0] != 1+len(catalog)+len(semanticControls) {
		t.Fatalf("first batch %d, want the goal plus the catalog plus the controls", embedder.batches[0])
	}
	if embedder.batches[1] != 1 {
		t.Fatalf("second batch %d, want the goal alone", embedder.batches[1])
	}
	var rows int
	if err := service.store.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM skill_embeddings`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != len(catalog)+len(semanticControls) {
		t.Fatalf("%d cached vectors, want one per Skill and one per control", rows)
	}
}

// With no embedder the scorer is what it was before R-14. An embedding model is
// a second model to run and running none is a supported configuration.
func TestSkillRetrievalWorksWithNoEmbedder(t *testing.T) {
	service, _, cleanup := testAgentService(t, httptest.NewServer(http.NotFoundHandler()))
	defer cleanup()
	catalog := englishSkillCatalog()
	if semantic := service.skillRelevance(context.Background(), "round satang", catalog); semantic != nil {
		t.Fatalf("no embedder configured, got %+v", semantic)
	}
	if got := selectSkillBindings("round satang half up", catalog); len(got) == 0 {
		t.Fatal("the lexical path stopped working")
	}
}

// TestTheSemanticCutIsRelativeNotAbsolute pins the rule that took three
// attempts to get right, using the similarities actually measured with bge-m3
// on Thai:
//
//	question about batch size  vs its paraphrase   0.480
//	                           vs unrelated prose  0.419
//	question about a plan id   vs its paraphrase   0.604
//	                           vs unrelated prose  0.471
//
// Both rankings are correct and the values overlap: any fixed floor that keeps
// the first paraphrase (0.480) also admits the second question's noise (0.471).
// What separates them is standing above the band their own query sits on.
func TestTheSemanticCutIsRelativeNotAbsolute(t *testing.T) {
	batchQuery := map[string]float64{"right": 0.480, "noise": 0.419, "other": 0.402}
	planQuery := map[string]float64{"right": 0.604, "noise": 0.471, "other": 0.455}
	for name, cosines := range map[string]map[string]float64{"batch": batchQuery, "plan": planQuery} {
		bonus := skillSemanticBonus(cosines, nil)
		if _, ok := bonus["right"]; !ok {
			t.Fatalf("%s: the paraphrase earned nothing", name)
		}
		if _, ok := bonus["noise"]; ok {
			t.Fatalf("%s: unrelated prose earned %d", name, bonus["noise"])
		}
	}
	// A fixed floor cannot do both: the value that admits the plan query's noise
	// is below the value the batch query's right answer scored.
	if planQuery["noise"] <= batchQuery["right"] {
		return
	}
	t.Fatal("the recorded measurements no longer overlap; this rule needs remeasuring")
}

// The controls are what makes "nothing here matches" expressible, so they are
// what a catalog too small to have a spread of its own falls back on. Measured
// with bge-m3: a goal about the weather scored 0.405 against a release
// checklist while the catalog's own quartile sat at 0.342 -- the catalog said
// match, the controls said this is what unrelated text scores for this query.
func TestControlsFloorACatalogWithNoSpread(t *testing.T) {
	// Two Skills, one clearly ahead of the other, and both of them no better
	// than unrelated prose scores for this goal.
	cosines := map[string]float64{"a": 0.41, "b": 0.34}
	if bonus := skillSemanticBonus(cosines, nil); len(bonus) == 0 {
		t.Fatal("premise broken: without controls the catalog picks a winner")
	}
	controls := []float64{0.39, 0.40, 0.41, 0.42, 0.43}
	if bonus := skillSemanticBonus(cosines, controls); len(bonus) != 0 {
		t.Fatalf("a goal no closer than unrelated prose produced %+v", bonus)
	}
}

// The floors fail in opposite directions, so the higher of the two is used. A
// catalog of technical summaries all outscores a sentence about a cat, which
// makes the controls too low to separate anything within it.
func TestTheCatalogStillFloorsWhenControlsAreLow(t *testing.T) {
	cosines := map[string]float64{"right": 0.62, "near": 0.55, "far": 0.54, "other": 0.53}
	controls := []float64{0.20, 0.21, 0.22, 0.23, 0.24}
	bonus := skillSemanticBonus(cosines, controls)
	if _, ok := bonus["right"]; !ok {
		t.Fatal("the best entry earned nothing")
	}
	if _, ok := bonus["other"]; ok {
		t.Fatalf("a low control floor let everything through: %+v", bonus)
	}
}
