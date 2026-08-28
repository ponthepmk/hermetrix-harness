package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"hermetrix-harness/internal/embedding"
)

// TestRealEmbedderCrossesScripts is the measurement the fake cannot make.
//
// conceptEmbedder proves the wiring and the selection rule; it cannot prove
// that a real sentence model puts "ปัดเศษเงินบาทเป็นจำนวนเต็มสตางค์" near
// "Round Thai monetary values half up using satang integers", which is the
// entire claim R-14 rests on. Run it against a real endpoint:
//
//	HERMETRIX_EMBED_URL=http://127.0.0.1:11434/v1 go test ./internal/agent/ \
//	  -run TestRealEmbedderCrossesScripts -v
//
// Skipped by default. A test that silently passes when the model is absent
// would be a claim with no evidence behind it, which is the failure this whole
// file exists to stop making.
func TestRealEmbedderCrossesScripts(t *testing.T) {
	endpoint := os.Getenv("HERMETRIX_EMBED_URL")
	if endpoint == "" {
		t.Skip("set HERMETRIX_EMBED_URL to measure against a real embedding model")
	}
	model := os.Getenv("HERMETRIX_EMBED_MODEL")
	if model == "" {
		model = "bge-m3"
	}
	service, _, cleanup := testAgentService(t, httptest.NewServer(http.NotFoundHandler()))
	defer cleanup()
	ctx := context.Background()
	service.SetEmbedder(embedding.NewOpenAIEmbedder(nil, endpoint, model,
		os.Getenv("HERMETRIX_EMBED_API_KEY"), 0))

	// The catalog the corpus actually ran with: three Skills, all described in
	// English, two of them about rounding money.
	catalog := []SessionSkillBinding{
		{SkillID: "s1", VersionID: "v1", CanonicalName: "satang-rounding",
			Summary: "Round Thai money amounts half up in satang"},
		{SkillID: "s2", VersionID: "v2", CanonicalName: "money-rounding-thai",
			Summary: "Round Thai monetary values half up using satang integers"},
		{SkillID: "s3", VersionID: "v3", CanonicalName: "invoice-numbering",
			Summary: "Format Thai invoice numbers as INV plus five digits"},
	}
	cases := []struct {
		goal string
		want string
	}{
		{"ปัดเศษเงินบาทเป็นจำนวนเต็มสตางค์", "rounding"},
		{"แก้การปัดเศษสตางค์ให้ปัดครึ่งขึ้น", "rounding"},
		{"ออกเลขที่ใบกำกับภาษีให้ถูกรูปแบบ", "s3"},
	}
	reached := 0
	for _, testCase := range cases {
		if lexical := selectSkillBindings(testCase.goal, catalog); len(lexical) != 0 {
			t.Fatalf("premise broken: %q already retrieves lexically", testCase.goal)
		}
		semantic := service.skillRelevance(ctx, testCase.goal, catalog)
		selected := rankSkillBindings(testCase.goal, catalog, semantic)
		if len(selected) == 0 {
			t.Errorf("%s -> [] (bonus %v)", testCase.goal, semantic)
			continue
		}
		top := selected[0].SkillID
		hit := top == testCase.want || (testCase.want == "rounding" && (top == "s1" || top == "s2"))
		t.Logf("%s -> %s (bonus %v)", testCase.goal, top, semantic)
		if !hit {
			t.Errorf("%s ranked %s first, want %s", testCase.goal, top, testCase.want)
			continue
		}
		reached++
	}
	if reached != len(cases) {
		t.Fatalf("%d/%d Thai goals reached the right Skill", reached, len(cases))
	}

	// Precision, on the catalog size where the catalog's own spread stopped being
	// a usable floor. With eight Skills a goal about the weather scored 0.405
	// against the release checklist while the lower quartile sat at 0.342, so it
	// cleared the margin and loaded two Skills the turn had no use for. The
	// controls are what closes that, and this is the case that measures it.
	wider := append(catalog,
		SessionSkillBinding{SkillID: "s4", VersionID: "v4", CanonicalName: "release-checklist",
			Summary: "Deploy the service to production safely"},
		SessionSkillBinding{SkillID: "s5", VersionID: "v5", CanonicalName: "db-backup",
			Summary: "Take a daily snapshot of the database and verify it restores"},
		SessionSkillBinding{SkillID: "s6", VersionID: "v6", CanonicalName: "log-triage",
			Summary: "Group repeated error lines and rank them by first occurrence"},
		SessionSkillBinding{SkillID: "s7", VersionID: "v7", CanonicalName: "pr-review",
			Summary: "Review a pull request for missing tests and unclear naming"},
		SessionSkillBinding{SkillID: "s8", VersionID: "v8", CanonicalName: "vat-report",
			Summary: "Produce the monthly sales tax report from the ledger"})
	for _, goal := range []string{
		"วันนี้อากาศเป็นยังไงบ้าง",
		"แมวของผมชอบนอนกลางแดด",
	} {
		for _, against := range [][]SessionSkillBinding{catalog, wider} {
			semantic := service.skillRelevance(ctx, goal, against)
			if selected := rankSkillBindings(goal, against, semantic); len(selected) != 0 {
				t.Errorf("unrelated goal %q retrieved %s from a catalog of %d (bonus %v)",
					goal, selected[0].SkillID, len(against), semantic)
			}
		}
	}
	// Recall must survive the wider catalog too: the right Skill still ranks
	// first when there are seven other things it could have picked.
	for _, testCase := range []struct{ goal, want string }{
		{"เขียนสคริปต์สำรองฐานข้อมูลรายวัน", "s5"},
		{"ปัดเศษเงินบาทเป็นจำนวนเต็มสตางค์", "s2"},
	} {
		semantic := service.skillRelevance(ctx, testCase.goal, wider)
		selected := rankSkillBindings(testCase.goal, wider, semantic)
		if len(selected) == 0 || selected[0].SkillID != testCase.want {
			t.Errorf("%s -> %+v, want %s first", testCase.goal, selected, testCase.want)
		}
	}
}
