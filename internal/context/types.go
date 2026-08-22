package context

import "time"

type Kind string

const (
	KindIdentity           Kind = "identity"
	KindPolicy             Kind = "policy"
	KindUserGoal           Kind = "user_goal"
	KindAcceptanceCriteria Kind = "acceptance_criteria"
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
}

type Request struct {
	Profile            Profile    `json:"profile"`
	Fragments          []Fragment `json:"fragments"`
	DirectTools        []ToolSpec `json:"direct_tools"`
	WorstCaseToolBurst int        `json:"worst_case_tool_burst"`
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
	Profile            string                `json:"profile"`
	TotalContext       int                   `json:"total_context"`
	OutputReserve      int                   `json:"output_reserve"`
	UncertaintyReserve int                   `json:"uncertainty_reserve"`
	WorstCaseToolBurst int                   `json:"worst_case_tool_burst"`
	PredictedInput     int                   `json:"predicted_input"`
	Free               int                   `json:"free"`
	OriginalTokens     int                   `json:"original_tokens"`
	SelectedTokens     int                   `json:"selected_tokens"`
	CompactedTokens    int                   `json:"compacted_tokens"`
	DroppedTokens      int                   `json:"dropped_tokens"`
	CompressionRatio   float64               `json:"compression_ratio"`
	SelectedIDs        []string              `json:"selected_ids"`
	DroppedIDs         []string              `json:"dropped_ids"`
	Spilled            []SpillReceipt        `json:"spilled"`
	Slices             map[string]SliceUsage `json:"slices"`
	Warnings           []string              `json:"warnings"`
	Integrity          IntegrityReport       `json:"integrity"`
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
