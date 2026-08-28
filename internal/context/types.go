package context

import (
	"strings"
	"time"
)

type Kind string

const (
	KindIdentity Kind = "identity"
	KindPolicy   Kind = "policy"
	KindUserGoal Kind = "user_goal"
	// acceptance_criteria was defined here and consumed by sliceFor and
	// evaluateIntegrity, and nothing ever produced one -- not the agent, not a
	// fixture. A census of 772 compiled snapshots found it at max=0 (O-40), so
	// the Phase 9 gate asked for retention of constraints this system has never
	// had. The owner chose not to build a producer, and the kind goes with that
	// decision: a type consumed by two branches and produced by nobody is the
	// same claim the six schema-only tables were making (O-42), and it belongs
	// in the commit that builds it.
	KindProjectInstruction Kind = "project_instruction"
	KindSelectedSkill      Kind = "selected_skill"
	KindConversation       Kind = "conversation"
	KindToolCall           Kind = "tool_call"
	KindToolResult         Kind = "tool_result"
	KindDecision           Kind = "decision"
	KindOpenTask           Kind = "open_task"
	KindArtifactReceipt    Kind = "artifact_receipt"
	KindCheckpoint         Kind = "checkpoint"
)

type Fragment struct {
	ID         string            `json:"id"`
	Kind       Kind              `json:"kind"`
	Scope      string            `json:"scope"`
	Provenance string            `json:"provenance"`
	Trust      string            `json:"trust"`
	Version    string            `json:"version"`
	Priority   int               `json:"priority"`
	Pinned     bool              `json:"pinned"`
	CacheClass string            `json:"cache_class"`
	Content    string            `json:"content"`
	PairID     string            `json:"pair_id,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type ToolSpec struct {
	Name     string   `json:"name"`
	Schema   string   `json:"schema"`
	Revision string   `json:"revision"`
	Source   string   `json:"source"`
	Effects  []string `json:"effects,omitempty"`
	// Serialized is the exact bytes this tool contributes to the provider
	// request, wrapper and description included. Name plus schema undercounts
	// it, which lets a request pass the compiler budget and still overflow the
	// real context window. Empty falls back to name plus schema.
	Serialized string `json:"serialized,omitempty"`
}

// BillableText returns what the estimator should count for this tool.
func (t ToolSpec) BillableText() string {
	if strings.TrimSpace(t.Serialized) != "" {
		return t.Serialized
	}
	return t.Name + "\n" + t.Schema
}

type Request struct {
	Profile            Profile    `json:"profile"`
	Fragments          []Fragment `json:"fragments"`
	DirectTools        []ToolSpec `json:"direct_tools"`
	WorstCaseToolBurst int        `json:"worst_case_tool_burst"`
	// MessageOverhead and RequestOverhead come from the provider's measured
	// chat template. Zero means unmeasured, and the compiler charges nothing
	// rather than guessing.
	MessageOverhead int `json:"message_overhead"`
	RequestOverhead int `json:"request_overhead"`
	// SemanticRelevance optionally scores one fragment against the session's
	// goal in [0,1], used when ranking what a checkpoint keeps. Nil means
	// lexical ranking only, which is the supported configuration wherever no
	// embedder is running. Not serialised: it is a callback, not state.
	SemanticRelevance func(fragmentID string) SemanticHint `json:"-"`
}

type SliceUsage struct {
	Budget int `json:"budget"`
	Used   int `json:"used"`
}

type SpillReceipt struct {
	Ref      string `json:"ref"`
	MIME     string `json:"mime"`
	Bytes    int    `json:"bytes"`
	Checksum string `json:"checksum"`
}

type Report struct {
	Profile            string `json:"profile"`
	TotalContext       int    `json:"total_context"`
	OutputReserve      int    `json:"output_reserve"`
	UncertaintyReserve int    `json:"uncertainty_reserve"`
	WorstCaseToolBurst int    `json:"worst_case_tool_burst"`
	// TransportTokens is what the chat template costs: a per-message wrapper
	// plus a per-request constant. Billed on every request and never part of
	// the context, so it is added to the prediction and kept out of the
	// content ledger, which balances what came in against where it went.
	TransportTokens int `json:"transport_tokens"`
	// PredictedPrompt is what this request is expected to cost: the selected
	// context plus the tool schemas. PredictedInput adds WorstCaseToolBurst,
	// which is budget held back for a tool result that has not happened yet.
	//
	// Only the first is comparable to the usage a provider reports. Measuring
	// the error band against the second made it look size-dependent -- from
	// -51.7% on a small context to -27.9% on a large one, purely because a
	// fixed reserve was being diluted by a growing prompt. Against the prompt
	// those same eighteen requests are a flat -21.5% with a 2.0% spread, which
	// is a bias a calibration can remove rather than noise it cannot.
	PredictedPrompt    int `json:"predicted_prompt"`
	PredictedInput     int `json:"predicted_input"`
	Free               int `json:"free"`
	OriginalTokens     int `json:"original_tokens"`
	SelectedTokens     int `json:"selected_tokens"`
	CompactedTokens    int `json:"compacted_tokens"`
	DroppedTokens      int `json:"dropped_tokens"`
	DeduplicatedTokens int `json:"deduplicated_tokens"`
	// DeduplicatedFragments is the count behind DeduplicatedTokens. The two must
	// agree about whether anything happened: a token total is easy to balance by
	// attributing a discrepancy to whichever term nobody counts separately, and
	// dedup was that term. The same reasoning pairs SpilledTokens with Spilled.
	DeduplicatedFragments int `json:"deduplicated_fragments"`
	SpilledTokens         int `json:"spilled_tokens"`
	// UnaccountedTokens closes the ledger. Every token that entered the compiler
	// left it in exactly one way: deduplicated, spilled to an artifact, selected,
	// or dropped. This field is that identity written down, and it must be zero.
	//
	// It was not written down before, and the report was unreadable because of
	// it: a live session showed 34,038 tokens in, 10,794 selected, 0 compacted
	// and 0 dropped, with 23,244 simply missing. Spill had absorbed them, but
	// nothing in the report said so in tokens, so "compaction never fires" looked
	// like a compactor bug for as long as it took to read the arithmetic.
	//
	// A context compiler whose whole claim is a certified budget cannot lose
	// two thirds of its input off the books.
	UnaccountedTokens int                   `json:"unaccounted_tokens"`
	CompressionRatio  float64               `json:"compression_ratio"`
	SelectedIDs       []string              `json:"selected_ids"`
	DroppedIDs        []string              `json:"dropped_ids"`
	Spilled           []SpillReceipt        `json:"spilled"`
	Slices            map[string]SliceUsage `json:"slices"`
	Warnings          []string              `json:"warnings"`
	Integrity         IntegrityReport       `json:"integrity"`
}

type IntegrityReport struct {
	PinnedTotal          int     `json:"pinned_total"`
	PinnedRetained       int     `json:"pinned_retained"`
	EssentialRetention   float64 `json:"essential_retention"`
	CausalPairsTotal     int     `json:"causal_pairs_total"`
	CausalPairsSelected  int     `json:"causal_pairs_selected"`
	CausalPairsCompacted int     `json:"causal_pairs_compacted"`
	CausalPairsOmitted   int     `json:"causal_pairs_omitted"`
}

type Compiled struct {
	Fragments   []Fragment `json:"fragments"`
	DirectTools []ToolSpec `json:"direct_tools"`
	Report      Report     `json:"report"`
}
