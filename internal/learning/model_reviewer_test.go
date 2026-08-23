package learning

import (
	"context"
	"strings"
	"testing"
)

// --- O-9: evidence must be able to become a candidate ---
//
// The parser is the safety boundary. Everything it accepts becomes a candidate
// that a human will be asked to promote, so anything it cannot read completely
// has to fall back to no_change rather than half-create something.

func TestReviewerParserFailsClosed(t *testing.T) {
	cases := []struct{ name, raw, wants string }{
		{"no object at all", "I think we should keep this.", "no decision object"},
		{"cut off mid-object", `{"kind":"create","reason":`, "no decision object"},
		{"braces but not valid json", `{kind: create, reason: none}`, "did not parse"},
		{"declines", `{"kind":"no_change","reason":"nothing reusable here"}`, "nothing reusable here"},
		{"unknown kind", `{"kind":"archive","reason":"drop it"}`, "drop it"},
		{"create with no name", `{"kind":"create","reason":"r","description":"d","markdown":"---\nname: x\n---\nbody"}`, "without a name"},
		{"create with no body", `{"kind":"create","reason":"r","canonical_name":"x","description":"d"}`, "without a name"},
		{"create with no description", `{"kind":"create","reason":"r","canonical_name":"x","markdown":"---\nname: x\n---\nbody"}`, "without a name"},
		{"body missing frontmatter", `{"kind":"create","reason":"r","canonical_name":"x","description":"d","markdown":"# Procedure\n1. do it"}`, "frontmatter naming x"},
		{"body names another skill", `{"kind":"create","reason":"r","canonical_name":"x","description":"d","markdown":"---\nname: y\n---\nbody"}`, "frontmatter naming x"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			decision, err := parseReviewerDecision(testCase.raw)
			if err != nil {
				t.Fatal(err)
			}
			if decision.Kind != "no_change" {
				t.Fatalf("kind %q, want no_change", decision.Kind)
			}
			if decision.SuggestedSkill != nil {
				t.Fatalf("a rejected decision still carried a proposal: %+v", decision.SuggestedSkill)
			}
			if !strings.Contains(decision.Reason, testCase.wants) {
				t.Fatalf("reason %q does not mention %q", decision.Reason, testCase.wants)
			}
		})
	}
}

func TestReviewerParserAcceptsACompleteProposal(t *testing.T) {
	raw := "Here is my decision:\n" +
		`{"kind":"create","reason":"the user had to correct the rounding twice",` +
		`"canonical_name":"satang-rounding","description":"Round Thai money amounts half up in satang",` +
		`"markdown":"---\nname: satang-rounding\ndescription: \"Round Thai money amounts half up in satang\"\ntags: []\ntools: []\n---\n\n# Procedure\n\n1. Keep amounts as integers.\n"}`
	decision, err := parseReviewerDecision(raw)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind != "create" {
		t.Fatalf("kind %q, want create", decision.Kind)
	}
	suggested := decision.SuggestedSkill
	if suggested == nil {
		t.Fatal("create decision carried no proposal, so the runner has nothing to write")
	}
	if suggested.CanonicalName != "satang-rounding" || suggested.ChangeKind != "create" {
		t.Fatalf("proposal is malformed: %+v", suggested)
	}
	if !strings.HasPrefix(suggested.Markdown, "---") {
		t.Fatalf("proposal body lost its frontmatter: %q", suggested.Markdown)
	}
	if len(decision.Risks) == 0 {
		t.Fatal("a model-written proposal must carry its risk note")
	}
}

// A digest that already contains a written procedure needs no model call, so
// the API path keeps working with the same reviewer installed.
func TestReviewerPassesThroughAnExplicitProcedure(t *testing.T) {
	reviewer := NewModelReviewer(nil, "")
	decision, err := reviewer.Review(context.Background(), Digest{
		SuggestedSkill: &SuggestedSkill{CanonicalName: "given", ChangeKind: "create",
			Reason: "handed over by the caller", Markdown: "---\nname: given\n---\n\n# Procedure\n\n1. step\n"}})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind != "create" {
		t.Fatalf("an explicitly supplied procedure was dropped: %+v", decision)
	}
}

// With no provider there is nothing to ask, and that must read as "could not
// review" rather than "there was nothing worth keeping".
func TestReviewerWithoutAProviderSaysSo(t *testing.T) {
	reviewer := NewModelReviewer(nil, "")
	// The digest has to be worth reviewing, or the deterministic filter answers
	// first and the provider is never consulted.
	decision, err := reviewer.Review(context.Background(), Digest{GoalAndConstraints: "do a thing",
		Outcome: "success", UserCorrections: []string{"event:a"}})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind != "no_change" || !strings.Contains(decision.Reason, "no review provider") {
		t.Fatalf("missing provider was not reported: %+v", decision)
	}
}

// --- O-15: do not pay a model call to be told nothing happened ---
//
// successful_milestone fires on any turn that read a file, which is nearly
// every turn. Seven varied turns driven live produced seven review calls and
// zero candidates. On a local-first harness the reviewer shares a device with
// foreground work, so that cost is the one that matters.

func TestReadOnlyWorkSkipsTheModelCall(t *testing.T) {
	// The provider ID points at nothing. Reaching a decision at all proves the
	// deterministic filter answered before anything tried to call a model.
	reviewer := NewModelReviewer(nil, "provider-that-does-not-exist")
	decision, err := reviewer.Review(context.Background(), Digest{
		GoalAndConstraints: "read the README and summarise it", Outcome: "success",
		ToolReceipts: []string{
			"event:a:workspace.list_files:succeeded",
			"event:b:workspace.read_file:succeeded",
			"event:c:workspace.search_files:succeeded",
		}})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind != "no_change" {
		t.Fatalf("kind %q, want no_change", decision.Kind)
	}
	if !strings.Contains(decision.Reason, "nothing durable") {
		t.Fatalf("read-only work still went to the model: %+v", decision)
	}
}

func TestWorkWorthReviewingStillReachesTheModel(t *testing.T) {
	cases := []struct {
		name   string
		digest Digest
	}{
		{"a correction", Digest{Outcome: "success", UserCorrections: []string{"event:a"}}},
		{"a Skill was in play", Digest{Outcome: "success", SkillActivations: []string{"skill:x@v1"}}},
		{"an explicit request", Digest{GoalAndConstraints: "จำไว้ว่าต้องทำแบบนี้", Outcome: "success"}},
		{"something was written", Digest{Outcome: "success",
			ToolReceipts: []string{"event:a:workspace.write_file:succeeded"}}},
		{"a remote capability ran", Digest{Outcome: "success",
			ToolReceipts: []string{"event:a:tool_call:succeeded"}}},
		// An unrecognised receipt must send the digest onward rather than be
		// dropped: the filter may only skip work it is sure about.
		{"an unparseable receipt", Digest{Outcome: "success", ToolReceipts: []string{"garbled"}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if reason, worth := worthAModelCall(testCase.digest); !worth {
				t.Fatalf("filter skipped work that deserved review: %q", reason)
			}
		})
	}
}

// The trigger vocabulary has one home; both the agent's classifier and this
// filter read it.
func TestTriggerVocabularyIsSharedAndCaseInsensitive(t *testing.T) {
	if !CorrectionRequested("ผิด report.py ยังหารด้วยศูนย์ได้") {
		t.Fatal("a Thai correction was not recognised")
	}
	if !CorrectionRequested("That's Wrong, try again") {
		t.Fatal("an English correction was not recognised regardless of case")
	}
	if CorrectionRequested("please summarise the report") {
		t.Fatal("an ordinary request was read as a correction")
	}
	if !ExplicitLearnRequested("จำไว้ว่าต้องทำแบบนี้") || !ExplicitLearnRequested("Remember this procedure") {
		t.Fatal("an explicit request to keep something was not recognised")
	}
	if ExplicitLearnRequested("what files are here") {
		t.Fatal("an ordinary question was read as a request to learn")
	}
}
