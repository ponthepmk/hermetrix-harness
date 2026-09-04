package learning

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"hermetrix-harness/internal/providers"
)

// ModelReviewer reads a digest and decides whether the work contains a bounded,
// reusable procedure worth writing down.
//
// StructuredReviewer, the first implementation, could only acknowledge a
// procedure someone else had already written. Nothing in the runtime ever wrote
// one, so evidence from real work could not become a Skill candidate at all --
// the queue, the outbox and the review job all worked and always ended in
// no_change. This is the piece that was missing.
//
// It stays inside the authority ladder. The output is a proposal: the runner
// turns it into a candidate that passes the same lint and security checks as
// any other, and a human still has to promote it. Nothing here can change
// active knowledge.
//
// Digest text is untrusted -- a goal is whatever the user typed, and a
// malicious one could ask for a Skill that says something harmful. That is
// contained by the same boundary rather than by trusting the model: a proposal
// is visible, reviewable and inert until someone approves it.
type ModelReviewer struct {
	providers  *providers.Service
	providerID string
}

func NewModelReviewer(providerService *providers.Service, providerID string) *ModelReviewer {
	return &ModelReviewer{providers: providerService, providerID: providerID}
}

func (r *ModelReviewer) Revision() string { return "model-reviewer-v1" }

// reviewerOutputBudget must hold reasoning plus a whole Skill body. Reasoning
// models bill their thinking as completion tokens, so a budget sized for the
// answer alone truncates the answer -- the same mistake that made the
// qualification suite report a recall failure that was really a budget failure.
const reviewerOutputBudget = 4096

// reviewerInstruction was measured against the labelled P8-A corpus and changed
// once on that evidence.
//
// The first version asked for a procedure and required that "the work followed
// steps". Against a hundred cases whose labels a second reader agreed with
// twenty out of twenty, it recalled 0.55 against a floor of 0.60 while
// producing no false proposal in forty-five negatives and inventing no
// evidence. It was not confused; it was applying a narrower definition of a
// Skill than the project holds.
//
// Two families showed the same gap from opposite sides. A user saying in so
// many words to remember a convention was declined as "no steps performed". A
// fix performed and written to a file was declined as "a one-off, file-specific
// edit". The reviewer wanted a convention and steps together, and no trigger
// family produces both.
//
// This version says what a Skill is rather than what shape the evidence takes,
// and keeps the two guards that were working: nothing invented, and a summary
// of a run is not knowledge. Whether it helps is a measurement, not a claim --
// and it is measured once. Tuning a prompt against the corpus until the corpus
// passes would fit the prompt to the hundred cases rather than to the job.
const reviewerInstruction = `You review one completed unit of work and decide whether it contains knowledge worth keeping for next time.

Answer with one JSON object and nothing else:

{"kind":"no_change","reason":"..."}

or

{"kind":"create","reason":"...","canonical_name":"kebab-case-name","description":"one sentence","markdown":"---\nname: kebab-case-name\ndescription: \"one sentence\"\ntags: []\ntools: []\n---\n\n# Procedure\n\n1. ...\n"}

Keep something when ANY of these holds:
- the user asked, in so many words, to remember a rule or a convention
- the user corrected the same point more than once
- the work applied a stated convention rather than only satisfying one request

Choose no_change when the evidence is a summary of what happened, an answer to a
question that was asked, or a fact about one value in one file and nothing more.

What you write down must be usable next time by someone who was not here, with
no value that belonged only to this run. A convention counts even when no steps
were performed: "money is kept as integer satang and rounded half up only at
display" is knowledge, and a turn that only stated it still carries it.

Evidence carries a verified_by list. It cites receipts that measured the
outcome: a command that actually completed and exited zero. When verified_by is
empty the outcome was asserted, not measured, and any procedure you propose
must say so in its own text—one plain sentence saying the approach was not
verified by a run. Do not describe unmeasured work as tested, verified or proven.

Do not invent steps the evidence does not show. Keep the body under 40 lines.`

func (r *ModelReviewer) Review(ctx context.Context, digest Digest) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	// An explicitly supplied procedure needs no model call.
	if digest.SuggestedSkill != nil && strings.TrimSpace(digest.SuggestedSkill.Markdown) != "" {
		return StructuredReviewer{}.Review(ctx, digest)
	}
	if reason, worth := worthAModelCall(digest); !worth {
		return Decision{Kind: "no_change", Reason: reason}, nil
	}
	if r.providers == nil || strings.TrimSpace(r.providerID) == "" {
		return Decision{Kind: "no_change", Reason: "no review provider is configured"}, nil
	}
	profile, err := r.providers.Get(ctx, r.providerID)
	if err != nil {
		return Decision{}, fmt.Errorf("load review provider: %w", err)
	}
	evidence, err := json.Marshal(digest)
	if err != nil {
		return Decision{}, err
	}
	temperature := 0.0
	completion, err := r.providers.StreamChat(ctx, profile, providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "system", Content: reviewerInstruction},
			{Role: "user", Content: "Evidence from one unit of work:\n" + string(evidence)},
		},
		Temperature: &temperature, MaxTokens: reviewerOutputBudget}, nil)
	if err != nil {
		return Decision{}, fmt.Errorf("review provider call: %w", err)
	}
	return parseReviewerDecision(completion.Content)
}

var jsonObjectPattern = regexp.MustCompile(`(?s)\{.*\}`)

// parseReviewerDecision fails closed. Anything it cannot read as a complete,
// well-formed proposal becomes no_change, because a malformed proposal is not
// evidence that a procedure exists.
func parseReviewerDecision(raw string) (Decision, error) {
	match := jsonObjectPattern.FindString(raw)
	if match == "" {
		return Decision{Kind: "no_change", Unusable: true, Reason: "reviewer returned no decision object"}, nil
	}
	var parsed struct {
		Kind          string `json:"kind"`
		Reason        string `json:"reason"`
		CanonicalName string `json:"canonical_name"`
		Description   string `json:"description"`
		Markdown      string `json:"markdown"`
	}
	if err := json.Unmarshal([]byte(match), &parsed); err != nil {
		return Decision{Kind: "no_change", Unusable: true, Reason: "reviewer decision did not parse: " + err.Error()}, nil
	}
	reason := strings.TrimSpace(parsed.Reason)
	if reason == "" {
		reason = "reviewer gave no reason"
	}
	if parsed.Kind != "create" {
		return Decision{Kind: "no_change", Reason: reason}, nil
	}
	name := strings.TrimSpace(parsed.CanonicalName)
	markdown := strings.TrimSpace(parsed.Markdown)
	if name == "" || markdown == "" || strings.TrimSpace(parsed.Description) == "" {
		return Decision{Kind: "no_change", Unusable: true, Reason: "reviewer proposed a Skill without a name, description or body"}, nil
	}
	if !strings.HasPrefix(markdown, "---") || !strings.Contains(markdown, "name: "+name) {
		return Decision{Kind: "no_change", Unusable: true,
			Reason: "reviewer body does not carry frontmatter naming " + name}, nil
	}
	return Decision{Kind: "create", Reason: reason,
		ExpectedBenefit: "reuse a procedure observed in completed work",
		Risks:           []string{"proposed by a model from evidence; needs lint, security checks, review and explicit promotion"},
		SuggestedSkill: &SuggestedSkill{CanonicalName: name, ScopeKind: "user", Owner: "user",
			ChangeKind: "create", Reason: reason, Markdown: markdown + "\n"}}, nil
}

// worthAModelCall is the deterministic filter that runs before the expensive
// one. successful_milestone fires on any turn that read a file, which is nearly
// every turn: driving seven varied turns produced seven review calls and zero
// candidates. On a local-first harness the reviewer competes with foreground
// work for the same device, so paying a model call to be told "nothing here" is
// the cost that matters.
//
// This is the project's own rule -- deterministic reduction before a semantic
// call -- applied where it was missing. It only skips work the model would
// almost certainly decline: a turn with no correction, no effect on anything,
// and no Skill in play has no procedure in it to find.
func worthAModelCall(digest Digest) (string, bool) {
	if len(digest.UserCorrections) > 0 {
		return "", true
	}
	if len(digest.SkillActivations) > 0 {
		return "", true
	}
	if ExplicitLearnRequested(digest.GoalAndConstraints) {
		return "", true
	}
	for _, receipt := range digest.ToolReceipts {
		if !isReadOnlyReceipt(receipt) {
			return "", true
		}
	}
	return "read-only work with no correction, Skill or explicit request: nothing durable to propose", false
}

// readOnlyToolNames are the primitives that observe without changing anything.
// A turn built only from these cannot have established a procedure worth
// keeping -- it looked at things.
var readOnlyToolNames = map[string]bool{
	"workspace.list_files":   true,
	"workspace.read_file":    true,
	"workspace.search_files": true,
	"skill_search":           true,
	"skill_view":             true,
	"tool_search":            true,
	"tool_describe":          true,
}

// isReadOnlyReceipt reads the "event:<id>:<tool>:<status>" shape the runtime
// writes. Anything it cannot parse counts as not read-only, so an unrecognised
// receipt sends the digest to the model rather than silently dropping it.
func isReadOnlyReceipt(receipt string) bool {
	parts := strings.Split(receipt, ":")
	if len(parts) < 4 {
		return false
	}
	return readOnlyToolNames[parts[len(parts)-2]]
}
