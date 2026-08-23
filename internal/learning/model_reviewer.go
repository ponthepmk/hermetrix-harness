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

const reviewerInstruction = `You review one completed unit of work and decide whether it contains a procedure worth keeping.

Answer with one JSON object and nothing else:

{"kind":"no_change","reason":"..."}

or

{"kind":"create","reason":"...","canonical_name":"kebab-case-name","description":"one sentence","markdown":"---\nname: kebab-case-name\ndescription: \"one sentence\"\ntags: []\ntools: []\n---\n\n# Procedure\n\n1. ...\n"}

Choose no_change unless ALL of these hold:
- the work followed steps that would be the same next time, not facts about one file
- the steps can be written without any value that was specific to this run
- someone doing this task again would get it wrong without them

A summary of what happened is not a procedure. A correction the user had to make usually is.
Do not invent steps that the evidence does not show. Keep the body under 40 lines.`

func (r *ModelReviewer) Review(ctx context.Context, digest Digest) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	// An explicitly supplied procedure needs no model call.
	if digest.SuggestedSkill != nil && strings.TrimSpace(digest.SuggestedSkill.Markdown) != "" {
		return StructuredReviewer{}.Review(ctx, digest)
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
		return Decision{Kind: "no_change", Reason: "reviewer returned no decision object"}, nil
	}
	var parsed struct {
		Kind          string `json:"kind"`
		Reason        string `json:"reason"`
		CanonicalName string `json:"canonical_name"`
		Description   string `json:"description"`
		Markdown      string `json:"markdown"`
	}
	if err := json.Unmarshal([]byte(match), &parsed); err != nil {
		return Decision{Kind: "no_change", Reason: "reviewer decision did not parse: " + err.Error()}, nil
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
		return Decision{Kind: "no_change", Reason: "reviewer proposed a Skill without a name, description or body"}, nil
	}
	if !strings.HasPrefix(markdown, "---") || !strings.Contains(markdown, "name: "+name) {
		return Decision{Kind: "no_change",
			Reason: "reviewer body does not carry frontmatter naming " + name}, nil
	}
	return Decision{Kind: "create", Reason: reason,
		ExpectedBenefit: "reuse a procedure observed in completed work",
		Risks:           []string{"proposed by a model from evidence; needs lint, security checks, review and explicit promotion"},
		SuggestedSkill: &SuggestedSkill{CanonicalName: name, ScopeKind: "user", Owner: "user",
			ChangeKind: "create", Reason: reason, Markdown: markdown + "\n"}}, nil
}
